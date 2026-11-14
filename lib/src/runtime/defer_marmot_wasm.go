package runtime

// wasmDeferCallee is the code address of the function a deferred call is
// about to invoke, set by its invoker (the panic loop, a defer thunk, an
// open defer runner) and taken by a function that calls recover at its
// entry: wasm keeps no frame records for recover to check its caller with
// (the code generator's wasmRecoverToken). One thread runs Go code, and
// nothing runs between the store and the callee's entry.
var wasmDeferCallee uintptr
