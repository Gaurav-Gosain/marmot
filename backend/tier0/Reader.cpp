// Reader.cpp: see Reader.h. A recursive descent reader over the text with
// caches keyed by the text of types, attribute dictionaries and locations.
// Operations are built with their ODS builders, so properties and their
// defaults are those the custom parsers set; the differential mode in the
// pipeline (MARMOT_PARSE_CHECK=1) compares the result with the parser's.

#include "tier0/Reader.h"

#include "mlir/AsmParser/AsmParser.h"
#include "mlir/Dialect/LLVMIR/LLVMDialect.h"
#include "mlir/Dialect/LLVMIR/LLVMTypes.h"
#include "mlir/IR/Builders.h"
#include "mlir/IR/BuiltinAttributes.h"
#include "mlir/IR/BuiltinTypes.h"
#include "llvm/ADT/DenseMap.h"
#include "llvm/ADT/StringMap.h"
#include "llvm/ADT/StringExtras.h"
#include "llvm/ADT/StringSwitch.h"

using namespace mlir;
namespace LL = mlir::LLVM;

namespace marmot {
namespace tier0 {
namespace {

struct Fail {
  std::string why;
};

class Reader {
public:
  Reader(MLIRContext *ctx, StringRef text, StringRef name)
      : ctx(ctx), b(ctx), p(text.begin()), start(text.begin()), end(text.end()),
        lineStart(text.begin()), file(StringAttr::get(ctx, name)) {}

  OwningOpRef<ModuleOp> run();

private:
  MLIRContext *ctx;
  OpBuilder b;
  const char *p, *start, *end, *lineStart;
  unsigned line = 1;
  StringAttr file;
  Location unknownLoc = UnknownLoc::get(ctx);

  llvm::StringMap<Type> typeAliases;      // !mtN
  llvm::StringMap<Location> locAliases;   // #locN
  llvm::StringMap<Type> typeCache;        // type text -> type
  llvm::StringMap<Attribute> attrCache;   // attribute text -> attribute
  // Per function (or global initializer) scope.
  llvm::DenseMap<StringRef, Value> values;
  llvm::DenseMap<StringRef, Block *> blocks;
  llvm::SmallVector<Block *> pendingBlocks;  // referenced, not defined yet

  [[noreturn]] void fail(const std::string &why) {
    throw Fail{why + " at line " + std::to_string(line)};
  }

  // ---- lexing ----
  void skipSpace() {
    while (p < end) {
      char c = *p;
      if (c == '\n') {
        ++line;
        lineStart = ++p;
      } else if (c == ' ' || c == '\t' || c == '\r') {
        ++p;
      } else if (c == '/' && p + 1 < end && p[1] == '/') {
        while (p < end && *p != '\n') ++p;
      } else {
        break;
      }
    }
  }
  bool peek(char c) {
    skipSpace();
    return p < end && *p == c;
  }
  bool consume(char c) {
    if (!peek(c)) return false;
    ++p;
    return true;
  }
  void expect(char c) {
    if (!consume(c)) fail(std::string("expected '") + c + "'");
  }
  // A keyword followed by a non identifier character.
  bool consumeWord(StringRef w) {
    skipSpace();
    if (size_t(end - p) < w.size() || StringRef(p, w.size()) != w) return false;
    const char *q = p + w.size();
    if (q < end && (isalnum(*q) || *q == '_' || *q == '.' || *q == '$')) return false;
    p = q;
    return true;
  }
  void expectWord(StringRef w) {
    if (!consumeWord(w)) fail("expected " + w.str());
  }
  static bool identChar(char c) { return isalnum(c) || c == '_' || c == '.' || c == '$' || c == '-'; }
  StringRef bareWord() {
    skipSpace();
    const char *s = p;
    while (p < end && (isalnum(*p) || *p == '_' || *p == '.' || *p == '$')) ++p;
    if (p == s) fail("expected a word");
    return StringRef(s, p - s);
  }
  // %name, ^name, @name, #name, !name: the text after the sigil.
  StringRef sigilName(char sigil) {
    skipSpace();
    if (p >= end || *p != sigil) fail(std::string("expected '") + sigil + "'");
    const char *s = p++;
    while (p < end && identChar(*p)) ++p;
    if (p == s + 1) fail("empty name");
    return StringRef(s, p - s);
  }
  // @name or @"name" (without escapes): the name.
  StringRef symName() {
    skipSpace();
    if (p + 1 < end && p[0] == '@' && p[1] == '"') {
      ++p;
      StringRef q = rawString().drop_front().drop_back();
      if (!q.contains('\\')) return q;
      // MLIR string escapes: \\, \", \n, \t and \XX (hex byte).
      std::string d;
      for (size_t i = 0; i < q.size(); i++) {
        char c = q[i];
        if (c != '\\') {
          d.push_back(c);
          continue;
        }
        if (++i >= q.size()) fail("bad escape");
        char e = q[i];
        if (e == '\\' || e == '"') d.push_back(e);
        else if (e == 'n') d.push_back('\n');
        else if (e == 't') d.push_back('\t');
        else if (isxdigit(e) && i + 1 < q.size() && isxdigit(q[i + 1])) {
          d.push_back(char(llvm::hexDigitValue(e) * 16 + llvm::hexDigitValue(q[i + 1])));
          ++i;
        } else fail("bad escape");
      }
      return StringAttr::get(ctx, d).getValue();
    }
    return sigilName('@').drop_front();
  }
  int64_t integer() {
    skipSpace();
    bool neg = false;
    if (p < end && *p == '-') {
      neg = true;
      ++p;
    }
    if (p >= end || !isdigit(*p)) fail("expected an integer");
    if (*p == '0' && p + 1 < end && (p[1] == 'x' || p[1] == 'X')) fail("hex integer");
    uint64_t v = 0;
    while (p < end && isdigit(*p)) {
      uint64_t nv = v * 10 + uint64_t(*p - '0');
      if (nv / 10 != v) fail("integer overflow");
      v = nv;
      ++p;
    }
    return neg ? -int64_t(v) : int64_t(v);
  }
  // A string literal with its quotes; escapes are kept (callers that need
  // the value give it to the MLIR attribute parser).
  StringRef rawString() {
    skipSpace();
    if (p >= end || *p != '"') fail("expected a string");
    const char *s = p++;
    while (p < end && *p != '"') {
      if (*p == '\\') ++p;
      if (*p == '\n') fail("newline in string");
      ++p;
    }
    if (p >= end) fail("unterminated string");
    ++p;
    return StringRef(s, p - s);
  }
  // The text of a balanced construct starting at p (brackets, parens,
  // braces and angle brackets, skipping strings), for the MLIR parser.
  StringRef balanced() {
    skipSpace();
    const char *s = p;
    int depth = 0;
    while (p < end) {
      char c = *p;
      if (c == '"') {
        rawString();
        continue;
      }
      if (c == '\n') fail("newline in attribute");
      if (c == '(' || c == '[' || c == '{' || c == '<') ++depth;
      if (c == ')' || c == ']' || c == '}' || c == '>') {
        if (c == '>' && p > s && p[-1] == '-') {  // "->" inside a function type
          ++p;
          continue;
        }
        --depth;
      }
      ++p;
      if (depth == 0) break;
    }
    if (depth != 0) fail("unbalanced");
    return StringRef(s, p - s);
  }

  Location here(const char *at) {
    // The parser's location for an operation without one: 1-based line and
    // column of its first token.
    return FileLineColLoc::get(file, line, unsigned(at - lineStart) + 1);
  }

  // ---- types ----
  Type type() {
    skipSpace();
    // Aggregate types repeat: cache them by their text.
    if (p < end && (*p == '!' || *p == 's' || *p == 'a') && !(p + 3 < end && StringRef(p, 3) == "!mt") &&
        !(p + 8 < end && StringRef(p, 9) == "!llvm.ptr")) {
      const char *s0 = p;
      const char *q = p;
      while (q < end && *q != '<' && (isalnum(*q) || *q == '!' || *q == '.' || *q == '_')) ++q;
      if (q < end && *q == '<') {
        int depth = 0;
        const char *e = q;
        for (; e < end; ++e) {
          if (*e == '<') ++depth;
          else if (*e == '>' && e[-1] != '-' && --depth == 0) break;
          else if (*e == '\n' || *e == '"') { e = end; break; }
        }
        if (e < end) {
          StringRef key(s0, size_t(e + 1 - s0));
          auto it = typeCache.find(key);
          if (it != typeCache.end()) {
            p = e + 1;
            return it->second;
          }
          Type t = typeUncached();
          if (p == e + 1) typeCache[key] = t;
          return t;
        }
      }
    }
    return typeUncached();
  }
  Type typeUncached() {
    skipSpace();
    const char *s = p;
    if (consumeWord("ptr") || consumeWord("!llvm.ptr")) return LL::LLVMPointerType::get(ctx);
    if (p < end && *p == 'i' && p + 1 < end && isdigit(p[1])) {
      ++p;
      int64_t w = integer();
      if (w < 1 || w > 128) fail("integer width");
      return IntegerType::get(ctx, unsigned(w));
    }
    if (consumeWord("f64")) return Float64Type::get(ctx);
    if (consumeWord("f32")) return Float32Type::get(ctx);
    if (p + 3 < end && StringRef(p, 3) == "!mt") {
      StringRef n = sigilName('!');
      auto it = typeAliases.find(n);
      if (it == typeAliases.end()) fail("unknown type alias");
      return it->second;
    }
    bool llvmPrefix = consumeWord("!llvm.struct") || (p = s, consumeWord("struct"));
    if (llvmPrefix) {
      expect('<');
      bool packed = consumeWord("packed");
      if (peek('"')) fail("identified struct");
      expect('(');
      SmallVector<Type> elems;
      if (!consume(')')) {
        do elems.push_back(type());
        while (consume(','));
        expect(')');
      }
      expect('>');
      return LL::LLVMStructType::getLiteral(ctx, elems, packed);
    }
    p = s;
    if (consumeWord("!llvm.array") || (p = s, consumeWord("array"))) {
      expect('<');
      int64_t n = integer();
      skipSpace();
      if (p >= end || *p != 'x') fail("expected x");
      ++p;
      Type e = type();
      expect('>');
      return LL::LLVMArrayType::get(e, unsigned(n));
    }
    p = s;
    fail("unknown type");
  }

  // ---- attributes ----
  // An attribute dictionary or other attribute through the MLIR parser,
  // cached by its text (most are the same few strings).
  Attribute parsedAttr(StringRef text) {
    auto [it, fresh] = attrCache.try_emplace(text, Attribute());
    if (fresh) {
      size_t n = 0;
      Attribute a = parseAttribute(text, ctx, Type(), &n, /*isKnownNullTerminated=*/false);
      if (!a || n != text.size()) {
        attrCache.erase(text);
        fail("attribute " + text.str());
      }
      it->second = a;
    }
    return it->second;
  }
  DictionaryAttr dict() {
    StringRef t = balanced();
    auto d = dyn_cast<DictionaryAttr>(parsedAttr(t));
    if (!d) fail("expected a dictionary");
    return d;
  }

  // ---- values and blocks ----
  Value use() {
    StringRef n = sigilName('%');
    auto it = values.find(n);
    if (it == values.end()) fail("forward reference " + n.str());
    return it->second;
  }
  void def(StringRef n, Value v) {
    if (!values.try_emplace(n, v).second) fail("redefinition " + n.str());
  }
  Block *blockRef() {
    StringRef n = sigilName('^');
    Block *&blk = blocks[n];
    if (!blk) {
      blk = new Block();
      pendingBlocks.push_back(blk);
    }
    return blk;
  }
  // ^bb(%a, %b : T, U) operands, or none.
  SmallVector<Value> succOperands() {
    SmallVector<Value> vs;
    if (!consume('(')) return vs;
    do vs.push_back(use());
    while (consume(','));
    expect(':');
    for (size_t i = 0; i < vs.size(); i++) {
      if (i) expect(',');
      if (type() != vs[i].getType()) fail("successor operand type");
    }
    expect(')');
    return vs;
  }
  void checkType(Value v, Type t) {
    if (v.getType() != t) fail("operand type");
  }

  // Optional trailing loc(#locN) or loc("file":l:c).
  Location trailingLoc(Location dflt) {
    skipSpace();
    if (!consumeWord("loc")) return dflt;
    expect('(');
    Location l = locBody();
    expect(')');
    return l;
  }
  Location locBody() {
    skipSpace();
    if (peek('#')) {
      StringRef n = sigilName('#');
      auto it = locAliases.find(n);
      if (it == locAliases.end()) fail("unknown location alias");
      return it->second;
    }
    if (peek('"')) {
      StringRef s = rawString();
      if (s.contains('\\')) fail("escaped file name");
      expect(':');
      int64_t l = integer();
      expect(':');
      int64_t c = integer();
      return FileLineColLoc::get(StringAttr::get(ctx, s.drop_front().drop_back()), unsigned(l), unsigned(c));
    }
    fail("location");
  }

  // ---- operations ----
  void topLevel(ModuleOp m);
  void func(ModuleOp m, Location at);
  void global(ModuleOp m, Location at);
  void region(Region &r, ArrayRef<std::pair<StringRef, Type>> entryArgs, ArrayRef<Location> argLocs);
  void operation();
  Operation *genericOp(SmallVectorImpl<StringRef> &results, const char *at);
  LL::Linkage linkage();
};

LL::Linkage Reader::linkage() {
  skipSpace();
  const char *s = p;
  if (p >= end || !isalpha(*p)) return LL::Linkage::External;
  StringRef w = bareWord();
  auto l = llvm::StringSwitch<std::optional<LL::Linkage>>(w)
               .Case("private", LL::Linkage::Private)
               .Case("internal", LL::Linkage::Internal)
               .Case("linkonce_odr", LL::Linkage::LinkonceODR)
               .Case("weak_odr", LL::Linkage::WeakODR)
               .Case("linkonce", LL::Linkage::Linkonce)
               .Case("weak", LL::Linkage::Weak)
               .Case("appending", LL::Linkage::Appending)
               .Case("external", LL::Linkage::External)
               .Case("extern_weak", LL::Linkage::ExternWeak)
               .Case("common", LL::Linkage::Common)
               .Case("available_externally", LL::Linkage::AvailableExternally)
               .Default(std::nullopt);
  if (!l) {
    p = s;
    return LL::Linkage::External;
  }
  return *l;
}

OwningOpRef<ModuleOp> Reader::run() {
  // Aliases and comments come before "module {".
  for (;;) {
    skipSpace();
    if (p >= end) fail("no module");
    if (*p == '!') {
      StringRef n = sigilName('!');
      expect('=');
      Type t = type();
      if (!typeAliases.try_emplace(n, t).second) fail("type alias redefined");
      continue;
    }
    if (*p == '#') {
      StringRef n = sigilName('#');
      expect('=');
      expectWord("loc");
      expect('(');
      Location l = locBody();
      expect(')');
      if (!locAliases.try_emplace(n, l).second) fail("location alias redefined");
      continue;
    }
    break;
  }
  const char *at = p;
  expectWord("module");
  if (!peek('{')) fail("module attributes");
  OwningOpRef<ModuleOp> m(ModuleOp::create(here(at)));
  expect('{');
  b.setInsertionPointToEnd(m->getBody());
  while (!consume('}')) topLevel(*m);
  Location ml = trailingLoc(m->getLoc());
  m->getOperation()->setLoc(ml);
  skipSpace();
  // Aliases may also follow the module.
  while (p < end) {
    if (*p != '#') fail("text after the module");
    StringRef n = sigilName('#');
    expect('=');
    expectWord("loc");
    expect('(');
    locBody();
    expect(')');
    (void)n;
    fail("location alias after the module");
  }
  return m;
}

void Reader::topLevel(ModuleOp m) {
  skipSpace();
  Location at = here(p);
  if (consumeWord("llvm.func")) return func(m, at);
  if (consumeWord("llvm.mlir.global")) return global(m, at);
  fail("top level operation");
}

void Reader::func(ModuleOp m, Location at) {
  LL::Linkage link = linkage();
  if (consumeWord("hidden") || consumeWord("protected") || consumeWord("dso_local") ||
      consumeWord("local_unnamed_addr") || consumeWord("unnamed_addr"))
    fail("function keyword");
  StringRef name = symName();
  expect('(');
  SmallVector<std::pair<StringRef, Type>> args;
  SmallVector<Location> argLocs;
  SmallVector<Type> argTypes;
  SmallVector<DictionaryAttr> argAttrs;
  bool anyArgAttr = false, named = false;
  if (!consume(')')) {
    do {
      skipSpace();
      if (peek('.')) fail("variadic function");
      StringRef an;
      const char *ap = p;
      if (peek('%')) {
        ap = p;
        an = sigilName('%');
        named = true;
        expect(':');
      }
      Type t = type();
      DictionaryAttr d;
      if (peek('{')) {
        d = dict();
        anyArgAttr = true;
      }
      if (peek('l')) fail("argument location");
      args.push_back({an, t});
      argLocs.push_back(here(ap));
      argTypes.push_back(t);
      argAttrs.push_back(d ? d : DictionaryAttr::get(ctx));
    } while (consume(','));
    expect(')');
  }
  Type result = LL::LLVMVoidType::get(ctx);
  DictionaryAttr resAttr;
  skipSpace();
  if (p + 1 < end && p[0] == '-' && p[1] == '>') {
    p += 2;
    if (consume('(')) {
      result = type();
      if (peek('{')) resAttr = dict();
      expect(')');
    } else {
      result = type();
    }
  }
  DictionaryAttr attrs;
  if (consumeWord("attributes")) attrs = dict();
  auto fty = LL::LLVMFunctionType::get(result, argTypes, false);
  b.setInsertionPointToEnd(m.getBody());
  auto fn = LL::LLVMFuncOp::create(b, at, name, fty, link);
  fn.setUnnamedAddr(LL::UnnamedAddr::None);  // as the parser sets it
  if (anyArgAttr) fn.setAllArgAttrs(argAttrs);
  if (resAttr) fn.setResAttrsAttr(ArrayAttr::get(ctx, {resAttr}));
  if (attrs)
    for (NamedAttribute na : attrs) fn->setAttr(na.getName(), na.getValue());
  if (peek('{')) {
    if (!named && !args.empty()) fail("unnamed arguments with a body");
    ++p;
    region(fn.getBody(), args, argLocs);
  } else if (named) {
    fail("named arguments without a body");
  }
  fn->setLoc(trailingLoc(fn.getLoc()));
  b.setInsertionPointToEnd(m.getBody());
}

void Reader::global(ModuleOp m, Location at) {
  LL::Linkage link = linkage();
  std::optional<LL::UnnamedAddr> ua;
  if (consumeWord("unnamed_addr")) ua = LL::UnnamedAddr::Global;
  else if (consumeWord("local_unnamed_addr")) ua = LL::UnnamedAddr::Local;
  if (consumeWord("thread_local") || consumeWord("dso_local") || consumeWord("hidden") ||
      consumeWord("externally_initialized"))
    fail("global keyword");
  bool constant = consumeWord("constant");
  StringRef name = symName();
  expect('(');
  Attribute value;
  if (!peek(')')) {
    if (!peek('"')) fail("global value");
    value = parsedAttr(rawString());
  }
  expect(')');
  DictionaryAttr attrs;
  if (peek('{')) attrs = dict();
  Type t;
  if (consume(':')) t = type();
  else if (auto s = dyn_cast_or_null<StringAttr>(value))
    t = LL::LLVMArrayType::get(IntegerType::get(ctx, 8), unsigned(s.getValue().size()));
  else
    fail("global without a type");
  b.setInsertionPointToEnd(m.getBody());
  auto g = LL::GlobalOp::create(b, at, t, constant, link, name, value);
  g.setUnnamedAddr(ua ? *ua : LL::UnnamedAddr::None);  // the parser always sets it
  if (attrs)
    for (NamedAttribute na : attrs) g->setAttr(na.getName(), na.getValue());
  if (peek('{')) {
    ++p;
    region(g.getInitializerRegion(), {}, {});
  }
  g->setLoc(trailingLoc(g.getLoc()));
  b.setInsertionPointToEnd(m.getBody());
}

// The body after its '{', to the matching '}'.
void Reader::region(Region &r, ArrayRef<std::pair<StringRef, Type>> entryArgs, ArrayRef<Location> argLocs) {
  values.clear();
  blocks.clear();
  pendingBlocks.clear();
  Block *entry = new Block();
  r.push_back(entry);
  for (size_t i = 0; i < entryArgs.size(); i++)
    def(entryArgs[i].first, entry->addArgument(entryArgs[i].second, argLocs[i]));
  b.setInsertionPointToEnd(entry);
  bool first = true;
  for (;;) {
    skipSpace();
    if (p >= end) fail("unterminated region");
    if (*p == '}') {
      ++p;
      break;
    }
    if (*p == '^') {
      StringRef n = sigilName('^');
      Block *&blk = blocks[n];
      if (first && entry->empty() && entryArgs.empty() && !blk) {
        blk = entry;  // a labeled entry block
      } else {
        if (!blk) {
          blk = new Block();
        } else {
          auto it = llvm::find(pendingBlocks, blk);
          if (it == pendingBlocks.end()) fail("block redefined");
          pendingBlocks.erase(it);
        }
        r.push_back(blk);
      }
      if (consume('(')) {
        do {
          skipSpace();
          Location al = here(p);
          StringRef an = sigilName('%');
          expect(':');
          Type t = type();
          if (peek('l')) fail("block argument location");
          def(an, blk->addArgument(t, al));
        } while (consume(','));
        expect(')');
      }
      expect(':');
      b.setInsertionPointToEnd(blk);
      first = false;
      continue;
    }
    first = false;
    operation();
  }
  if (!pendingBlocks.empty()) fail("undefined block");
}

static std::optional<LL::ICmpPredicate> icmpPred(StringRef s) { return LL::symbolizeICmpPredicate(s); }

void Reader::operation() {
  skipSpace();
  const char *at = p;
  SmallVector<StringRef, 1> results;
  if (peek('%')) {
    results.push_back(sigilName('%'));
    if (peek(':')) fail("result group");
    if (consume(',')) fail("several results");
    expect('=');
  }
  skipSpace();
  at = p;  // custom and generic operations get the location of their name
  unsigned atLine = line, atCol = unsigned(at - lineStart) + 1;
  Location loc0 = unknownLoc;
  Operation *op = nullptr;
  if (peek('"')) {
    op = genericOp(results, at);
  } else {
    StringRef name = bareWord();
    // A placeholder: nearly every operation has a trailing loc(...), so the
    // position location is made only when it has none (below).
    Location loc = unknownLoc;
    auto bin = [&](auto tag) {
      using OpT = decltype(tag);
      Value l = use();
      expect(',');
      Value r = use();
      expect(':');
      Type t = type();
      checkType(l, t);
      checkType(r, t);
      return OpT::create(b, loc, l, r).getOperation();
    };
    auto cast = [&](auto tag) {
      using OpT = decltype(tag);
      Value v = use();
      expect(':');
      checkType(v, type());
      expectWord("to");
      Type t = type();
      return OpT::create(b, loc, t, v).getOperation();
    };
    auto fun = [&](auto tag) {
      using OpT = decltype(tag);
      expect('(');
      SmallVector<Value> as;
      if (!consume(')')) {
        do as.push_back(use());
        while (consume(','));
        expect(')');
      }
      expect(':');
      expect('(');
      for (size_t i = 0; i < as.size(); i++) {
        if (i) expect(',');
        checkType(as[i], type());
      }
      expect(')');
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      Type rt = type();
      return OpT::create(b, loc, rt, as).getOperation();
    };
    if (name == "llvm.mlir.constant") {
      expect('(');
      Attribute v;
      skipSpace();
      const char *vs = p;
      if (consumeWord("true") || consumeWord("false")) {
        bool tv = *vs == 't';
        expect(')');
        expect(':');
        Type t = type();
        if (t != IntegerType::get(ctx, 1)) fail("bool constant type");
        v = BoolAttr::get(ctx, tv);
        op = LL::ConstantOp::create(b, loc, t, v);
      } else if (p < end && (isdigit(*p) || *p == '-') && !(p[0] == '0' && p + 1 < end && p[1] == 'x')) {
        int64_t n = integer();
        if (peek('.') || peek('e')) fail("float constant");
        expect(':');
        Type at2 = type();
        expect(')');
        expect(':');
        Type t = type();
        auto it = dyn_cast<IntegerType>(at2);
        if (!it || at2 != t) fail("constant type");
        APInt ap(64, uint64_t(n), true);
        if (it.getWidth() < 64) {
          if (!ap.isSignedIntN(it.getWidth()) && !ap.isIntN(it.getWidth())) fail("constant width");
          ap = ap.trunc(it.getWidth());
        } else if (it.getWidth() > 64) {
          ap = ap.sext(it.getWidth());
        }
        op = LL::ConstantOp::create(b, loc, t, IntegerAttr::get(t, ap));
      } else {
        p = vs - 1;  // back to '('
        StringRef inner = balanced();
        StringRef a = inner.drop_front().drop_back();
        expect(':');
        Type t = type();
        op = LL::ConstantOp::create(b, loc, t, parsedAttr(a.trim()));
      }
    } else if (name == "llvm.call") {
      skipSpace();
      Value fp;
      StringRef callee;
      if (peek('%')) fp = use();
      else callee = symName();
      expect('(');
      SmallVector<Value> as;
      if (!consume(')')) {
        do as.push_back(use());
        while (consume(','));
        expect(')');
      }
      DictionaryAttr attrs;
      if (peek('{')) attrs = dict();
      expect(':');
      if (fp) {
        checkType(fp, type());
        expect(',');
      }
      expect('(');
      SmallVector<Type> ats;
      for (size_t i = 0; i < as.size(); i++) {
        if (i) expect(',');
        ats.push_back(type());
        checkType(as[i], ats.back());
      }
      expect(')');
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      SmallVector<Type, 1> rts;
      if (consume('(')) {
        if (!consume(')')) fail("call results");
      } else {
        rts.push_back(type());
      }
      if ((rts.empty()) != results.empty()) fail("call result count");
      LL::CallOp c;
      if (fp) {
        SmallVector<Value> ops{fp};
        ops.append(as.begin(), as.end());
        auto fty = LL::LLVMFunctionType::get(rts.empty() ? Type(LL::LLVMVoidType::get(ctx)) : rts[0], ats, false);
        c = LL::CallOp::create(b, loc, fty, ops);
      } else {
        c = LL::CallOp::create(b, loc, TypeRange(rts), callee, as);
      }
      if (attrs)
        for (NamedAttribute na : attrs) {
          if (na.getName() != "no_inline") fail("call attribute");
          c->setAttr(na.getName(), na.getValue());
        }
      op = c;
    } else if (name == "llvm.insertvalue") {
      Value v = use();
      expect(',');
      Value c = use();
      expect('[');
      SmallVector<int64_t> pos;
      do pos.push_back(integer());
      while (consume(','));
      expect(']');
      expect(':');
      checkType(c, type());
      op = LL::InsertValueOp::create(b, loc, c, v, pos);
    } else if (name == "llvm.extractvalue") {
      Value c = use();
      expect('[');
      SmallVector<int64_t> pos;
      do pos.push_back(integer());
      while (consume(','));
      expect(']');
      expect(':');
      checkType(c, type());
      op = LL::ExtractValueOp::create(b, loc, c, pos);
    } else if (name == "llvm.cond_br") {
      Value c = use();
      std::optional<std::pair<uint32_t, uint32_t>> w;
      if (consumeWord("weights")) {
        expect('(');
        expect('[');
        int64_t a = integer();
        expect(',');
        int64_t bb = integer();
        expect(']');
        expect(')');
        w = {uint32_t(a), uint32_t(bb)};
      }
      expect(',');
      Block *t = blockRef();
      SmallVector<Value> to = succOperands();
      expect(',');
      Block *f = blockRef();
      SmallVector<Value> fo = succOperands();
      op = LL::CondBrOp::create(b, loc, c, t, to, f, fo, w);
    } else if (name == "llvm.br") {
      Block *d = blockRef();
      SmallVector<Value> os = succOperands();
      op = LL::BrOp::create(b, loc, os, d);
    } else if (name == "llvm.icmp" || name == "llvm.fcmp") {
      StringRef ps = rawString().drop_front().drop_back();
      Value l = use();
      expect(',');
      Value r = use();
      expect(':');
      Type t = type();
      checkType(l, t);
      checkType(r, t);
      if (name == "llvm.icmp") {
        auto pr = icmpPred(ps);
        if (!pr) fail("icmp predicate");
        op = LL::ICmpOp::create(b, loc, *pr, l, r);
      } else {
        auto pr = LL::symbolizeFCmpPredicate(ps);
        if (!pr) fail("fcmp predicate");
        op = LL::FCmpOp::create(b, loc, *pr, l, r);
      }
    } else if (name == "llvm.load") {
      bool vol = consumeWord("volatile");
      Value a = use();
      LL::AtomicOrdering ord = LL::AtomicOrdering::not_atomic;
      if (consumeWord("atomic")) {
        if (consumeWord("syncscope")) fail("syncscope");
        auto o = LL::symbolizeAtomicOrdering(bareWord());
        if (!o) fail("atomic ordering");
        ord = *o;
      }
      unsigned align = 0;
      bool unsafe = false;
      if (peek('{')) {
        for (NamedAttribute na : dict()) {
          if (na.getName() == "alignment") {
            auto ia = dyn_cast<IntegerAttr>(na.getValue());
            if (!ia || ia.getType() != IntegerType::get(ctx, 64)) fail("alignment");
            align = unsigned(ia.getInt());
          } else if (na.getName() == "go.unsafe" && isa<UnitAttr>(na.getValue())) {
            unsafe = true;
          } else {
            fail("load attribute");
          }
        }
      }
      expect(':');
      checkType(a, type());
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      Type t = type();
      auto l = LL::LoadOp::create(b, loc, t, a, align, vol, false, false, false, ord);
      if (unsafe) l->setAttr("go.unsafe", UnitAttr::get(ctx));
      op = l;
    } else if (name == "llvm.store") {
      bool vol = consumeWord("volatile");
      Value v = use();
      expect(',');
      Value a = use();
      LL::AtomicOrdering ord = LL::AtomicOrdering::not_atomic;
      if (consumeWord("atomic")) {
        if (consumeWord("syncscope")) fail("syncscope");
        auto o = LL::symbolizeAtomicOrdering(bareWord());
        if (!o) fail("atomic ordering");
        ord = *o;
      }
      unsigned align = 0;
      bool unsafe = false;
      if (peek('{')) {
        for (NamedAttribute na : dict()) {
          if (na.getName() == "alignment") {
            auto ia = dyn_cast<IntegerAttr>(na.getValue());
            if (!ia || ia.getType() != IntegerType::get(ctx, 64)) fail("alignment");
            align = unsigned(ia.getInt());
          } else if (na.getName() == "go.unsafe" && isa<UnitAttr>(na.getValue())) {
            unsafe = true;
          } else {
            fail("store attribute");
          }
        }
      }
      expect(':');
      checkType(v, type());
      expect(',');
      checkType(a, type());
      auto s = LL::StoreOp::create(b, loc, v, a, align, vol, false, false, ord);
      if (unsafe) s->setAttr("go.unsafe", UnitAttr::get(ctx));
      op = s;
    } else if (name == "llvm.getelementptr") {
      LL::GEPNoWrapFlags flags = LL::GEPNoWrapFlags::none;
      if (consumeWord("inbounds")) flags = LL::GEPNoWrapFlags::inbounds;
      skipSpace();
      if (!peek('%')) fail("gep flags");
      Value base = use();
      expect('[');
      SmallVector<LL::GEPArg> idx;
      SmallVector<Value> dyn;
      do {
        skipSpace();
        if (peek('%')) {
          Value v = use();
          idx.push_back(v);
          dyn.push_back(v);
        } else {
          int64_t n = integer();
          if (n < INT32_MIN || n > INT32_MAX) fail("gep index");
          idx.push_back(int32_t(n));
        }
      } while (consume(','));
      expect(']');
      expect(':');
      expect('(');
      checkType(base, type());
      for (Value v : dyn) {
        expect(',');
        checkType(v, type());
      }
      expect(')');
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      Type rt = type();
      expect(',');
      Type et = type();
      op = LL::GEPOp::create(b, loc, rt, et, base, idx, flags);
    } else if (name == "llvm.mlir.addressof") {
      StringRef g = symName();
      expect(':');
      Type t = type();
      op = LL::AddressOfOp::create(b, loc, t, g);
    } else if (name == "llvm.mlir.zero") {
      expect(':');
      op = LL::ZeroOp::create(b, loc, type());
    } else if (name == "llvm.mlir.undef") {
      expect(':');
      op = LL::UndefOp::create(b, loc, type());
    } else if (name == "llvm.unreachable") {
      op = LL::UnreachableOp::create(b, loc);
    } else if (name == "llvm.return") {
      SmallVector<Value, 1> vs;
      skipSpace();
      if (peek('%')) {
        vs.push_back(use());
        expect(':');
        checkType(vs[0], type());
      }
      op = LL::ReturnOp::create(b, loc, vs);
    } else if (name == "llvm.select") {
      Value c = use();
      expect(',');
      Value t = use();
      expect(',');
      Value f = use();
      expect(':');
      checkType(c, type());
      expect(',');
      Type ty = type();
      checkType(t, ty);
      checkType(f, ty);
      op = LL::SelectOp::create(b, loc, c, t, f);
    } else if (name == "llvm.alloca") {
      if (consumeWord("inalloca")) fail("inalloca");
      Value n = use();
      expectWord("x");
      Type et = type();
      unsigned align = 0;
      if (peek('{')) {
        for (NamedAttribute na : dict()) {
          auto ia = dyn_cast<IntegerAttr>(na.getValue());
          if (na.getName() != "alignment" || !ia || ia.getType() != IntegerType::get(ctx, 64))
            fail("alloca attribute");
          align = unsigned(ia.getInt());
        }
      }
      expect(':');
      expect('(');
      checkType(n, type());
      expect(')');
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      Type rt = type();
      op = LL::AllocaOp::create(b, loc, rt, et, n, align);
    } else if (name == "llvm.add") op = bin(LL::AddOp());
    else if (name == "llvm.sub") op = bin(LL::SubOp());
    else if (name == "llvm.mul") op = bin(LL::MulOp());
    else if (name == "llvm.and") op = bin(LL::AndOp());
    else if (name == "llvm.or") op = bin(LL::OrOp());
    else if (name == "llvm.xor") op = bin(LL::XOrOp());
    else if (name == "llvm.shl") op = bin(LL::ShlOp());
    else if (name == "llvm.lshr") op = bin(LL::LShrOp());
    else if (name == "llvm.ashr") op = bin(LL::AShrOp());
    else if (name == "llvm.sdiv") op = bin(LL::SDivOp());
    else if (name == "llvm.udiv") op = bin(LL::UDivOp());
    else if (name == "llvm.srem") op = bin(LL::SRemOp());
    else if (name == "llvm.urem") op = bin(LL::URemOp());
    else if (name == "llvm.fadd") op = bin(LL::FAddOp());
    else if (name == "llvm.fsub") op = bin(LL::FSubOp());
    else if (name == "llvm.fmul") op = bin(LL::FMulOp());
    else if (name == "llvm.fdiv") op = bin(LL::FDivOp());
    else if (name == "llvm.zext") op = cast(LL::ZExtOp());
    else if (name == "llvm.sext") op = cast(LL::SExtOp());
    else if (name == "llvm.trunc") op = cast(LL::TruncOp());
    else if (name == "llvm.ptrtoint") op = cast(LL::PtrToIntOp());
    else if (name == "llvm.inttoptr") op = cast(LL::IntToPtrOp());
    else if (name == "llvm.sitofp") op = cast(LL::SIToFPOp());
    else if (name == "llvm.uitofp") op = cast(LL::UIToFPOp());
    else if (name == "llvm.fptrunc") op = cast(LL::FPTruncOp());
    else if (name == "llvm.fpext") op = cast(LL::FPExtOp());
    else if (name == "llvm.fneg") {
      Value v = use();
      expect(':');
      checkType(v, type());
      op = LL::FNegOp::create(b, loc, v);
    } else if (name == "llvm.intr.fma") op = fun(LL::FMAOp());
    else if (name == "llvm.intr.minimum") op = fun(LL::MinimumOp());
    else if (name == "llvm.intr.maximum") op = fun(LL::MaximumOp());
    else if (name == "llvm.call_intrinsic") {
      StringRef in = rawString();
      if (in.contains('\\')) fail("escaped intrinsic name");
      expect('(');
      SmallVector<Value> as;
      if (!consume(')')) {
        do as.push_back(use());
        while (consume(','));
        expect(')');
      }
      expect(':');
      expect('(');
      for (size_t i = 0; i < as.size(); i++) {
        if (i) expect(',');
        checkType(as[i], type());
      }
      expect(')');
      skipSpace();
      if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
      p += 2;
      SmallVector<Type, 1> rts;
      if (consume('(')) {
        if (!consume(')')) fail("intrinsic results");
      } else {
        rts.push_back(type());
      }
      StringAttr ia = StringAttr::get(ctx, in.drop_front().drop_back());
      op = rts.empty() ? LL::CallIntrinsicOp::create(b, loc, ia, as)
                       : LL::CallIntrinsicOp::create(b, loc, rts[0], ia, as);
    } else if (name == "llvm.atomicrmw") {
      if (consumeWord("volatile")) fail("volatile atomicrmw");
      auto bo = LL::symbolizeAtomicBinOp(bareWord());
      if (!bo) fail("atomicrmw op");
      Value a = use();
      expect(',');
      Value v = use();
      auto ord = LL::symbolizeAtomicOrdering(bareWord());
      if (!ord) fail("atomic ordering");
      expect(':');
      checkType(a, type());
      expect(',');
      checkType(v, type());
      op = LL::AtomicRMWOp::create(b, loc, *bo, a, v, *ord);
    } else if (name == "llvm.cmpxchg") {
      if (consumeWord("weak") || consumeWord("volatile")) fail("cmpxchg kind");
      Value a = use();
      expect(',');
      Value c = use();
      expect(',');
      Value v = use();
      auto so = LL::symbolizeAtomicOrdering(bareWord());
      auto fo = LL::symbolizeAtomicOrdering(bareWord());
      if (!so || !fo) fail("atomic ordering");
      expect(':');
      checkType(a, type());
      expect(',');
      Type t = type();
      checkType(c, t);
      checkType(v, t);
      op = LL::AtomicCmpXchgOp::create(b, loc, a, c, v, *so, *fo);
    } else {
      fail("operation " + name.str());
    }
  }
  if (!op) fail("operation");
  if (op->getNumResults() != results.size()) fail("result count");
  if (!results.empty()) def(results[0], op->getResult(0));
  {
    skipSpace();
    if (p + 3 < end && StringRef(p, 4) == "loc(") op->setLoc(trailingLoc(loc0));
    else op->setLoc(FileLineColLoc::get(file, atLine, atCol));
  }
}

// "name"(operands) <{properties}> {attributes} : (types) -> (types)
Operation *Reader::genericOp(SmallVectorImpl<StringRef> &results, const char *at) {
  StringRef qn = rawString();
  if (qn.contains('\\')) fail("escaped operation name");
  StringRef name = qn.drop_front().drop_back();
  auto rn = RegisteredOperationName::lookup(name, ctx);
  if (!rn) fail("unregistered operation " + name.str());
  OperationState st(here(at), *rn);
  expect('(');
  if (!consume(')')) {
    do st.operands.push_back(use());
    while (consume(','));
    expect(')');
  }
  if (peek('[') || peek('(')) fail("generic successors or regions");
  skipSpace();
  if (p + 1 < end && p[0] == '<' && p[1] == '{') {
    ++p;  // '<'
    DictionaryAttr props = dict();
    expect('>');
    st.propertiesAttr = props;
  }
  if (peek('{')) st.addAttributes(dict().getValue());
  expect(':');
  expect('(');
  for (size_t i = 0; i < st.operands.size(); i++) {
    if (i) expect(',');
    checkType(st.operands[i], type());
  }
  expect(')');
  skipSpace();
  if (!(p + 1 < end && p[0] == '-' && p[1] == '>')) fail("expected ->");
  p += 2;
  if (consume('(')) {
    if (!consume(')')) {
      do st.types.push_back(type());
      while (consume(','));
      expect(')');
    }
  } else {
    st.types.push_back(type());
  }
  (void)results;
  Operation *op = Operation::create(st);
  if (!op) fail("generic operation");
  b.insert(op);
  return op;
}

} // namespace

OwningOpRef<ModuleOp> readModule(MLIRContext *ctx, StringRef text, StringRef name, std::string &why) {
  ctx->getOrLoadDialect<LL::LLVMDialect>();
  Reader r(ctx, text, name);
  try {
    return r.run();
  } catch (const Fail &f) {
    why = f.why;
    return nullptr;
  }
}

} // namespace tier0
} // namespace marmot
