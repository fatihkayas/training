// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reflectdata

import (
	"fmt"

	"cmd/compile/internal/base"
	"cmd/compile/internal/compare"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/objw"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"cmd/internal/obj"
	"cmd/internal/src"
)

// AlgType returns the fixed-width AMEMxx variants instead of the general
// AMEM kind when possible.
func AlgType(t *types.Type) types.AlgKind {
	a := types.AlgType(t)
	if a == types.AMEM {
		if t.Alignment() < int64(base.Ctxt.Arch.Alignment) && t.Alignment() < t.Size() {
			// For example, we can't treat [2]int16 as an int32 if int32s require
			// 4-byte alignment. See issue 46283.
			return a
		}
		switch t.Size() {
		case 0:
			return types.AMEM0
		case 1:
			return types.AMEM8
		case 2:
			return types.AMEM16
		case 4:
			return types.AMEM32
		case 8:
			return types.AMEM64
		case 16:
			return types.AMEM128
		}
	}

	return a
}

// genhash returns a symbol which is the closure used to compute
// the hash of a value of type t.
// Note: the generated function must match runtime.typehash exactly.
func genhash(t *types.Type) *obj.LSym {
	switch AlgType(t) {
	default:
		// genhash is only called for types that have equality
		base.Fatalf("genhash %v", t)
	case types.AMEM0:
		return sysClosure("memhash0")
	case types.AMEM8:
		return sysClosure("memhash8")
	case types.AMEM16:
		return sysClosure("memhash16")
	case types.AMEM32:
		return sysClosure("memhash32")
	case types.AMEM64:
		return sysClosure("memhash64")
	case types.AMEM128:
		return sysClosure("memhash128")
	case types.ASTRING:
		return sysClosure("strhash")
	case types.AINTER:
		return sysClosure("interhash")
	case types.ANILINTER:
		return sysClosure("nilinterhash")
	case types.AFLOAT32:
		return sysClosure("f32hash")
	case types.AFLOAT64:
		return sysClosure("f64hash")
	case types.ACPLX64:
		return sysClosure("c64hash")
	case types.ACPLX128:
		return sysClosure("c128hash")
	case types.AMEM:
		// For other sizes of plain memory, we build a closure
		// that calls memhash_varlen. The size of the memory is
		// encoded in the first slot of the closure.
		closure := TypeLinksymLookup(fmt.Sprintf(".hashfunc%d", t.Size()))
		if len(closure.P) > 0 { // already generated
			return closure
		}
		if memhashvarlen == nil {
			memhashvarlen = typecheck.LookupRuntimeFunc("memhash_varlen")
		}
		ot := 0
		ot = objw.SymPtr(closure, ot, memhashvarlen, 0)
		ot = objw.Uintptr(closure, ot, uint64(t.Size())) // size encoded in closure
		objw.Global(closure, int32(ot), obj.DUPOK|obj.RODATA)
		return closure
	case types.ASPECIAL:
		break
	}

	closure := TypeLinksymPrefix(".hashfunc", t)
	if len(closure.P) > 0 { // already generated
		return closure
	}

	// Generate hash functions for subtypes.
	// There are cases where we might not use these hashes,
	// but in that case they will get dead-code eliminated.
	// (And the closure generated by genhash will also get
	// dead-code eliminated, as we call the subtype hashers
	// directly.)
	switch t.Kind() {
	case types.TARRAY:
		genhash(t.Elem())
	case types.TSTRUCT:
		for _, f := range t.Fields() {
			genhash(f.Type)
		}
	}

	if base.Flag.LowerR != 0 {
		fmt.Printf("genhash %v %v\n", closure, t)
	}

	fn := hashFunc(t)

	// Build closure. It doesn't close over any variables, so
	// it contains just the function pointer.
	objw.SymPtr(closure, 0, fn.Linksym(), 0)
	objw.Global(closure, int32(types.PtrSize), obj.DUPOK|obj.RODATA)

	return closure
}

func hashFunc(t *types.Type) *ir.Func {
	sym := TypeSymPrefix(".hash", t)
	if sym.Def != nil {
		return sym.Def.(*ir.Name).Func
	}

	pos := base.AutogeneratedPos // less confusing than end of input
	base.Pos = pos

	// func sym(p *T, h uintptr) uintptr
	fn := ir.NewFunc(pos, pos, sym, types.NewSignature(nil,
		[]*types.Field{
			types.NewField(pos, typecheck.Lookup("p"), types.NewPtr(t)),
			types.NewField(pos, typecheck.Lookup("h"), types.Types[types.TUINTPTR]),
		},
		[]*types.Field{
			types.NewField(pos, nil, types.Types[types.TUINTPTR]),
		},
	))
	sym.Def = fn.Nname
	fn.Pragma |= ir.Noinline // TODO(mdempsky): We need to emit this during the unified frontend instead, to allow inlining.

	typecheck.DeclFunc(fn)
	np := fn.Dcl[0]
	nh := fn.Dcl[1]

	switch t.Kind() {
	case types.TARRAY:
		// An array of pure memory would be handled by the
		// standard algorithm, so the element type must not be
		// pure memory.
		hashel := hashfor(t.Elem())

		// for i := 0; i < nelem; i++
		ni := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
		init := ir.NewAssignStmt(base.Pos, ni, ir.NewInt(base.Pos, 0))
		cond := ir.NewBinaryExpr(base.Pos, ir.OLT, ni, ir.NewInt(base.Pos, t.NumElem()))
		post := ir.NewAssignStmt(base.Pos, ni, ir.NewBinaryExpr(base.Pos, ir.OADD, ni, ir.NewInt(base.Pos, 1)))
		loop := ir.NewForStmt(base.Pos, nil, cond, post, nil, false)
		loop.PtrInit().Append(init)

		// h = hashel(&p[i], h)
		call := ir.NewCallExpr(base.Pos, ir.OCALL, hashel, nil)

		nx := ir.NewIndexExpr(base.Pos, np, ni)
		nx.SetBounded(true)
		na := typecheck.NodAddr(nx)
		call.Args.Append(na)
		call.Args.Append(nh)
		loop.Body.Append(ir.NewAssignStmt(base.Pos, nh, call))

		fn.Body.Append(loop)

	case types.TSTRUCT:
		// Walk the struct using memhash for runs of AMEM
		// and calling specific hash functions for the others.
		for i, fields := 0, t.Fields(); i < len(fields); {
			f := fields[i]

			// Skip blank fields.
			if f.Sym.IsBlank() {
				i++
				continue
			}

			// Hash non-memory fields with appropriate hash function.
			if !compare.IsRegularMemory(f.Type) {
				hashel := hashfor(f.Type)
				call := ir.NewCallExpr(base.Pos, ir.OCALL, hashel, nil)
				na := typecheck.NodAddr(typecheck.DotField(base.Pos, np, i))
				call.Args.Append(na)
				call.Args.Append(nh)
				fn.Body.Append(ir.NewAssignStmt(base.Pos, nh, call))
				i++
				continue
			}

			// Otherwise, hash a maximal length run of raw memory.
			size, next := compare.Memrun(t, i)

			// h = hashel(&p.first, size, h)
			hashel := hashmem(f.Type)
			call := ir.NewCallExpr(base.Pos, ir.OCALL, hashel, nil)
			na := typecheck.NodAddr(typecheck.DotField(base.Pos, np, i))
			call.Args.Append(na)
			call.Args.Append(nh)
			call.Args.Append(ir.NewInt(base.Pos, size))
			fn.Body.Append(ir.NewAssignStmt(base.Pos, nh, call))

			i = next
		}
	}

	r := ir.NewReturnStmt(base.Pos, nil)
	r.Results.Append(nh)
	fn.Body.Append(r)

	if base.Flag.LowerR != 0 {
		ir.DumpList("genhash body", fn.Body)
	}

	typecheck.FinishFuncBody()

	fn.SetDupok(true)

	ir.WithFunc(fn, func() {
		typecheck.Stmts(fn.Body)
	})

	fn.SetNilCheckDisabled(true)

	return fn
}

func runtimeHashFor(name string, t *types.Type) *ir.Name {
	return typecheck.LookupRuntime(name, t)
}

// hashfor returns the function to compute the hash of a value of type t.
func hashfor(t *types.Type) *ir.Name {
	switch types.AlgType(t) {
	case types.AMEM:
		base.Fatalf("hashfor with AMEM type")
	case types.AINTER:
		return runtimeHashFor("interhash", t)
	case types.ANILINTER:
		return runtimeHashFor("nilinterhash", t)
	case types.ASTRING:
		return runtimeHashFor("strhash", t)
	case types.AFLOAT32:
		return runtimeHashFor("f32hash", t)
	case types.AFLOAT64:
		return runtimeHashFor("f64hash", t)
	case types.ACPLX64:
		return runtimeHashFor("c64hash", t)
	case types.ACPLX128:
		return runtimeHashFor("c128hash", t)
	}

	fn := hashFunc(t)
	return fn.Nname
}

// sysClosure returns a closure which will call the
// given runtime function (with no closed-over variables).
func sysClosure(name string) *obj.LSym {
	s := typecheck.LookupRuntimeVar(name + "·f")
	if len(s.P) == 0 {
		f := typecheck.LookupRuntimeFunc(name)
		objw.SymPtr(s, 0, f, 0)
		objw.Global(s, int32(types.PtrSize), obj.DUPOK|obj.RODATA)
	}
	return s
}

// geneq returns a symbol which is the closure used to compute
// equality for two objects of type t.
func geneq(t *types.Type) *obj.LSym {
	switch AlgType(t) {
	case types.ANOEQ, types.ANOALG:
		// The runtime will panic if it tries to compare
		// a type with a nil equality function.
		return nil
	case types.AMEM0:
		return sysClosure("memequal0")
	case types.AMEM8:
		return sysClosure("memequal8")
	case types.AMEM16:
		return sysClosure("memequal16")
	case types.AMEM32:
		return sysClosure("memequal32")
	case types.AMEM64:
		return sysClosure("memequal64")
	case types.AMEM128:
		return sysClosure("memequal128")
	case types.ASTRING:
		return sysClosure("strequal")
	case types.AINTER:
		return sysClosure("interequal")
	case types.ANILINTER:
		return sysClosure("nilinterequal")
	case types.AFLOAT32:
		return sysClosure("f32equal")
	case types.AFLOAT64:
		return sysClosure("f64equal")
	case types.ACPLX64:
		return sysClosure("c64equal")
	case types.ACPLX128:
		return sysClosure("c128equal")
	case types.AMEM:
		// make equality closure. The size of the type
		// is encoded in the closure.
		closure := TypeLinksymLookup(fmt.Sprintf(".eqfunc%d", t.Size()))
		if len(closure.P) != 0 {
			return closure
		}
		if memequalvarlen == nil {
			memequalvarlen = typecheck.LookupRuntimeFunc("memequal_varlen")
		}
		ot := 0
		ot = objw.SymPtr(closure, ot, memequalvarlen, 0)
		ot = objw.Uintptr(closure, ot, uint64(t.Size()))
		objw.Global(closure, int32(ot), obj.DUPOK|obj.RODATA)
		return closure
	case types.ASPECIAL:
		break
	}

	closure := TypeLinksymPrefix(".eqfunc", t)
	if len(closure.P) > 0 { // already generated
		return closure
	}

	if base.Flag.LowerR != 0 {
		fmt.Printf("geneq %v\n", t)
	}

	fn := eqFunc(t)

	// Generate a closure which points at the function we just generated.
	objw.SymPtr(closure, 0, fn.Linksym(), 0)
	objw.Global(closure, int32(types.PtrSize), obj.DUPOK|obj.RODATA)
	return closure
}

func eqFunc(t *types.Type) *ir.Func {
	// Autogenerate code for equality of structs and arrays.
	sym := TypeSymPrefix(".eq", t)
	if sym.Def != nil {
		return sym.Def.(*ir.Name).Func
	}

	pos := base.AutogeneratedPos // less confusing than end of input
	base.Pos = pos

	// func sym(p, q *T) bool
	fn := ir.NewFunc(pos, pos, sym, types.NewSignature(nil,
		[]*types.Field{
			types.NewField(pos, typecheck.Lookup("p"), types.NewPtr(t)),
			types.NewField(pos, typecheck.Lookup("q"), types.NewPtr(t)),
		},
		[]*types.Field{
			types.NewField(pos, typecheck.Lookup("r"), types.Types[types.TBOOL]),
		},
	))
	sym.Def = fn.Nname
	fn.Pragma |= ir.Noinline // TODO(mdempsky): We need to emit this during the unified frontend instead, to allow inlining.

	typecheck.DeclFunc(fn)
	np := fn.Dcl[0]
	nq := fn.Dcl[1]
	nr := fn.Dcl[2]

	// Label to jump to if an equality test fails.
	neq := typecheck.AutoLabel(".neq")

	// We reach here only for types that have equality but
	// cannot be handled by the standard algorithms,
	// so t must be either an array or a struct.
	switch t.Kind() {
	default:
		base.Fatalf("geneq %v", t)

	case types.TARRAY:
		nelem := t.NumElem()

		// checkAll generates code to check the equality of all array elements.
		// If unroll is greater than nelem, checkAll generates:
		//
		// if eq(p[0], q[0]) && eq(p[1], q[1]) && ... {
		// } else {
		//   goto neq
		// }
		//
		// And so on.
		//
		// Otherwise it generates:
		//
		// iterateTo := nelem/unroll*unroll
		// for i := 0; i < iterateTo; i += unroll {
		//   if eq(p[i+0], q[i+0]) && eq(p[i+1], q[i+1]) && ... && eq(p[i+unroll-1], q[i+unroll-1]) {
		//   } else {
		//     goto neq
		//   }
		// }
		// if eq(p[iterateTo+0], q[iterateTo+0]) && eq(p[iterateTo+1], q[iterateTo+1]) && ... {
		// } else {
		//    goto neq
		// }
		//
		checkAll := func(unroll int64, last bool, eq func(pi, qi ir.Node) ir.Node) {
			// checkIdx generates a node to check for equality at index i.
			checkIdx := func(i ir.Node) ir.Node {
				// pi := p[i]
				pi := ir.NewIndexExpr(base.Pos, np, i)
				pi.SetBounded(true)
				pi.SetType(t.Elem())
				// qi := q[i]
				qi := ir.NewIndexExpr(base.Pos, nq, i)
				qi.SetBounded(true)
				qi.SetType(t.Elem())
				return eq(pi, qi)
			}

			iterations := nelem / unroll
			iterateTo := iterations * unroll
			// If a loop is iterated only once, there shouldn't be any loop at all.
			if iterations == 1 {
				iterateTo = 0
			}

			if iterateTo > 0 {
				// Generate an unrolled for loop.
				// for i := 0; i < nelem/unroll*unroll; i += unroll
				i := typecheck.TempAt(base.Pos, ir.CurFunc, types.Types[types.TINT])
				init := ir.NewAssignStmt(base.Pos, i, ir.NewInt(base.Pos, 0))
				cond := ir.NewBinaryExpr(base.Pos, ir.OLT, i, ir.NewInt(base.Pos, iterateTo))
				loop := ir.NewForStmt(base.Pos, nil, cond, nil, nil, false)
				loop.PtrInit().Append(init)

				// if eq(p[i+0], q[i+0]) && eq(p[i+1], q[i+1]) && ... && eq(p[i+unroll-1], q[i+unroll-1]) {
				// } else {
				//   goto neq
				// }
				for j := int64(0); j < unroll; j++ {
					// if check {} else { goto neq }
					nif := ir.NewIfStmt(base.Pos, checkIdx(i), nil, nil)
					nif.Else.Append(ir.NewBranchStmt(base.Pos, ir.OGOTO, neq))
					loop.Body.Append(nif)
					post := ir.NewAssignStmt(base.Pos, i, ir.NewBinaryExpr(base.Pos, ir.OADD, i, ir.NewInt(base.Pos, 1)))
					loop.Body.Append(post)
				}

				fn.Body.Append(loop)

				if nelem == iterateTo {
					if last {
						fn.Body.Append(ir.NewAssignStmt(base.Pos, nr, ir.NewBool(base.Pos, true)))
					}
					return
				}
			}

			// Generate remaining checks, if nelem is not a multiple of unroll.
			if last {
				// Do last comparison in a different manner.
				nelem--
			}
			// if eq(p[iterateTo+0], q[iterateTo+0]) && eq(p[iterateTo+1], q[iterateTo+1]) && ... {
			// } else {
			//    goto neq
			// }
			for j := iterateTo; j < nelem; j++ {
				// if check {} else { goto neq }
				nif := ir.NewIfStmt(base.Pos, checkIdx(ir.NewInt(base.Pos, j)), nil, nil)
				nif.Else.Append(ir.NewBranchStmt(base.Pos, ir.OGOTO, neq))
				fn.Body.Append(nif)
			}
			if last {
				fn.Body.Append(ir.NewAssignStmt(base.Pos, nr, checkIdx(ir.NewInt(base.Pos, nelem))))
			}
		}

		switch t.Elem().Kind() {
		case types.TSTRING:
			// Do two loops. First, check that all the lengths match (cheap).
			// Second, check that all the contents match (expensive).
			checkAll(3, false, func(pi, qi ir.Node) ir.Node {
				// Compare lengths.
				eqlen, _ := compare.EqString(pi, qi)
				return eqlen
			})
			checkAll(1, true, func(pi, qi ir.Node) ir.Node {
				// Compare contents.
				_, eqmem := compare.EqString(pi, qi)
				return eqmem
			})
		case types.TFLOAT32, types.TFLOAT64:
			checkAll(2, true, func(pi, qi ir.Node) ir.Node {
				// p[i] == q[i]
				return ir.NewBinaryExpr(base.Pos, ir.OEQ, pi, qi)
			})
		case types.TSTRUCT:
			isCall := func(n ir.Node) bool {
				return n.Op() == ir.OCALL || n.Op() == ir.OCALLFUNC
			}
			var expr ir.Node
			var hasCallExprs bool
			allCallExprs := true
			and := func(cond ir.Node) {
				if expr == nil {
					expr = cond
				} else {
					expr = ir.NewLogicalExpr(base.Pos, ir.OANDAND, expr, cond)
				}
			}

			var tmpPos src.XPos
			pi := ir.NewIndexExpr(tmpPos, np, ir.NewInt(tmpPos, 0))
			pi.SetBounded(true)
			pi.SetType(t.Elem())
			qi := ir.NewIndexExpr(tmpPos, nq, ir.NewInt(tmpPos, 0))
			qi.SetBounded(true)
			qi.SetType(t.Elem())
			flatConds, canPanic := compare.EqStruct(t.Elem(), pi, qi)
			for _, c := range flatConds {
				if isCall(c) {
					hasCallExprs = true
				} else {
					allCallExprs = false
				}
			}
			if !hasCallExprs || allCallExprs || canPanic {
				checkAll(1, true, func(pi, qi ir.Node) ir.Node {
					// p[i] == q[i]
					return ir.NewBinaryExpr(base.Pos, ir.OEQ, pi, qi)
				})
			} else {
				checkAll(4, false, func(pi, qi ir.Node) ir.Node {
					expr = nil
					flatConds, _ := compare.EqStruct(t.Elem(), pi, qi)
					if len(flatConds) == 0 {
						return ir.NewBool(base.Pos, true)
					}
					for _, c := range flatConds {
						if !isCall(c) {
							and(c)
						}
					}
					return expr
				})
				checkAll(2, true, func(pi, qi ir.Node) ir.Node {
					expr = nil
					flatConds, _ := compare.EqStruct(t.Elem(), pi, qi)
					for _, c := range flatConds {
						if isCall(c) {
							and(c)
						}
					}
					return expr
				})
			}
		default:
			checkAll(1, true, func(pi, qi ir.Node) ir.Node {
				// p[i] == q[i]
				return ir.NewBinaryExpr(base.Pos, ir.OEQ, pi, qi)
			})
		}

	case types.TSTRUCT:
		flatConds, _ := compare.EqStruct(t, np, nq)
		if len(flatConds) == 0 {
			fn.Body.Append(ir.NewAssignStmt(base.Pos, nr, ir.NewBool(base.Pos, true)))
		} else {
			for _, c := range flatConds[:len(flatConds)-1] {
				// if cond {} else { goto neq }
				n := ir.NewIfStmt(base.Pos, c, nil, nil)
				n.Else.Append(ir.NewBranchStmt(base.Pos, ir.OGOTO, neq))
				fn.Body.Append(n)
			}
			fn.Body.Append(ir.NewAssignStmt(base.Pos, nr, flatConds[len(flatConds)-1]))
		}
	}

	// ret:
	//   return
	ret := typecheck.AutoLabel(".ret")
	fn.Body.Append(ir.NewLabelStmt(base.Pos, ret))
	fn.Body.Append(ir.NewReturnStmt(base.Pos, nil))

	// neq:
	//   r = false
	//   return (or goto ret)
	fn.Body.Append(ir.NewLabelStmt(base.Pos, neq))
	fn.Body.Append(ir.NewAssignStmt(base.Pos, nr, ir.NewBool(base.Pos, false)))
	if compare.EqCanPanic(t) || anyCall(fn) {
		// Epilogue is large, so share it with the equal case.
		fn.Body.Append(ir.NewBranchStmt(base.Pos, ir.OGOTO, ret))
	} else {
		// Epilogue is small, so don't bother sharing.
		fn.Body.Append(ir.NewReturnStmt(base.Pos, nil))
	}
	// TODO(khr): the epilogue size detection condition above isn't perfect.
	// We should really do a generic CL that shares epilogues across
	// the board. See #24936.

	if base.Flag.LowerR != 0 {
		ir.DumpList("geneq body", fn.Body)
	}

	typecheck.FinishFuncBody()

	fn.SetDupok(true)

	ir.WithFunc(fn, func() {
		typecheck.Stmts(fn.Body)
	})

	// Disable checknils while compiling this code.
	// We are comparing a struct or an array,
	// neither of which can be nil, and our comparisons
	// are shallow.
	fn.SetNilCheckDisabled(true)
	return fn
}

// EqFor returns ONAME node represents type t's equal function, and a boolean
// to indicates whether a length needs to be passed when calling the function.
func EqFor(t *types.Type) (ir.Node, bool) {
	switch types.AlgType(t) {
	case types.AMEM:
		return typecheck.LookupRuntime("memequal", t, t), true
	case types.ASPECIAL:
		fn := eqFunc(t)
		return fn.Nname, false
	}
	base.Fatalf("EqFor %v", t)
	return nil, false
}

func anyCall(fn *ir.Func) bool {
	return ir.Any(fn, func(n ir.Node) bool {
		// TODO(rsc): No methods?
		op := n.Op()
		return op == ir.OCALL || op == ir.OCALLFUNC
	})
}

func hashmem(t *types.Type) ir.Node {
	return typecheck.LookupRuntime("memhash", t)
}
