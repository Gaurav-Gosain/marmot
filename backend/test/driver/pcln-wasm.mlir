// A wasm object's pc table: function starts are code section offsets (the
// image base is 0), not wrapped below the lowest data address.
// REQUIRES: wasm
// RUN: %backend -O1 -target wasip1/wasm -o %t.o %S/debuginfo.mlir
// RUN: %pcln -o %t.pcln %t.o
// RUN: python3 -c "import struct,sys; t=open(sys.argv[1],'rb').read(); nf,te,fo=struct.unpack_from('<I',t,12)[0],struct.unpack_from('<I',t,32)[0],struct.unpack_from('<I',t,36)[0]; s=[struct.unpack_from('<I',t,fo+12*k)[0] for k in range(nf)]; print('funcs', nf>0, 'inside', all(x<te for x in s), 'end', te < 0x100000)" %t.pcln | %FileCheck %s
// CHECK: funcs True inside True end True
