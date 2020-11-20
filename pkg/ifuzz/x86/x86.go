// Copyright 2017 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

//go:generate bash -c "go run gen/gen.go gen/all-enc-instructions.txt > generated/insns.go"

// Package x86 allows to generate and mutate x86 machine code.
package x86

import (
	"math/rand"

	"github.com/google/syzkaller/pkg/ifuzz/ifuzzimpl"
)

type Insn struct {
	Name      string
	Extension string

	Mode   int  // bitmask of compatible modes
	Priv   bool // CPL=0
	Pseudo bool // pseudo instructions can consist of several real instructions

	Opcode      []byte
	Prefix      []byte
	Suffix      []byte
	Modrm       bool
	Mod         int8
	Reg         int8 // -6 - segment register, -8 - control register
	Rm          int8
	Srm         bool // register is embed in the first byte
	NoSibDisp   bool // no SIB/disp even if modrm says otherwise
	Imm         int8 // immediate size, -1 - immediate size, -2 - address size, -3 - operand size
	Imm2        int8
	NoRepPrefix bool
	No66Prefix  bool
	Rexw        int8 // 1 must be set, -1 must not be set
	Mem32       bool // instruction always references 32-bit memory operand, 0x67 is illegal
	Mem16       bool // instruction always references 16-bit memory operand

	Vex        byte
	VexMap     byte
	VexL       int8
	VexNoR     bool
	VexP       int8
	Avx2Gather bool

	generator func(cfg *ifuzzimpl.Config, r *rand.Rand) []byte // for pseudo instructions
}

type InsnSetX86 struct {
	modeInsns [ifuzzimpl.ModeLast][ifuzzimpl.TypeLast][]ifuzzimpl.Insn
	Insns     []*Insn
}

func Register(insns []*Insn) {
	if len(insns) == 0 {
		panic("no instructions")
	}
	insnset := &InsnSetX86{
		Insns: insns,
	}
	insnset.initPseudo()
	for mode := ifuzzimpl.Mode(0); mode < ifuzzimpl.ModeLast; mode++ {
		for _, insn := range insnset.Insns {
			if insn.Mode&(1<<uint(mode)) == 0 {
				continue
			}
			if insn.Pseudo {
				insnset.modeInsns[mode][ifuzzimpl.TypeExec] =
					append(insnset.modeInsns[mode][ifuzzimpl.TypeExec], insn)
			} else if insn.Priv {
				insnset.modeInsns[mode][ifuzzimpl.TypePriv] =
					append(insnset.modeInsns[mode][ifuzzimpl.TypePriv], insn)
				insnset.modeInsns[mode][ifuzzimpl.TypeAll] =
					append(insnset.modeInsns[mode][ifuzzimpl.TypeAll], insn)
			} else {
				insnset.modeInsns[mode][ifuzzimpl.TypeUser] =
					append(insnset.modeInsns[mode][ifuzzimpl.TypeUser], insn)
				insnset.modeInsns[mode][ifuzzimpl.TypeAll] =
					append(insnset.modeInsns[mode][ifuzzimpl.TypeAll], insn)
			}
		}
	}
	ifuzzimpl.Arches[ifuzzimpl.ArchX86] = insnset
}

func (insnset *InsnSetX86) GetInsns(mode ifuzzimpl.Mode, typ ifuzzimpl.Type) []ifuzzimpl.Insn {
	return insnset.modeInsns[mode][typ]
}

func (insn *Insn) Info() (string, bool) {
	return insn.Name, insn.Pseudo
}

func generateArg(cfg *ifuzzimpl.Config, r *rand.Rand, size int) []byte {
	v := generateInt(cfg, r, size)
	arg := make([]byte, size)
	for i := 0; i < size; i++ {
		arg[i] = byte(v)
		v >>= 8
	}
	return arg
}

func (insn *Insn) isCompatible(cfg *ifuzzimpl.Config) bool {
	if cfg.Mode < 0 || cfg.Mode >= ifuzzimpl.ModeLast {
		panic("bad mode")
	}
	if insn.Priv && !cfg.Priv {
		return false
	}
	if insn.Pseudo && !cfg.Exec {
		return false
	}
	if insn.Mode&(1<<uint(cfg.Mode)) == 0 {
		return false
	}
	return true
}

func generateInt(cfg *ifuzzimpl.Config, r *rand.Rand, size int) uint64 {
	if size != 1 && size != 2 && size != 4 && size != 8 {
		panic("bad arg size")
	}
	var v uint64
	switch x := r.Intn(60); {
	case x < 10:
		v = uint64(r.Intn(1 << 4))
	case x < 20:
		v = uint64(r.Intn(1 << 16))
	case x < 25:
		v = uint64(r.Int63()) % (1 << 32)
	case x < 30:
		v = uint64(r.Int63())
	case x < 40:
		v = ifuzzimpl.SpecialNumbers[r.Intn(len(ifuzzimpl.SpecialNumbers))]
		if r.Intn(5) == 0 {
			v += uint64(r.Intn(33)) - 16
		}
	case x < 50 && len(cfg.MemRegions) != 0:
		mem := cfg.MemRegions[r.Intn(len(cfg.MemRegions))]
		switch x := r.Intn(100); {
		case x < 25:
			v = mem.Start
		case x < 50:
			v = mem.Start + mem.Size
		case x < 75:
			v = mem.Start + mem.Size/2
		default:
			v = mem.Start + uint64(r.Int63())%mem.Size
		}
		if r.Intn(10) == 0 {
			v += uint64(r.Intn(33)) - 16
		}
	default:
		v = uint64(r.Intn(1 << 8))
	}
	if r.Intn(50) == 0 {
		v = uint64(-int64(v))
	}
	if r.Intn(50) == 0 && size != 1 {
		v &^= 1<<12 - 1
	}
	return v
}
