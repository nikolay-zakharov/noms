// Copyright 2016 Attic Labs, Inc. All rights reserved.
// Licensed under the Apache License, version 2.0:
// http://www.apache.org/licenses/LICENSE-2.0

package types

import (
	"fmt"

	"github.com/attic-labs/noms/go/d"
	"github.com/attic-labs/noms/go/hash"
)

type valueDecoder struct {
	nomsReader
	vr ValueReader
	tc *TypeCache
}

// |tc| must be locked as long as the valueDecoder is being used
func newValueDecoder(nr nomsReader, vr ValueReader, tc *TypeCache) *valueDecoder {
	return &valueDecoder{nr, vr, tc}
}

func (r *valueDecoder) readKind() NomsKind {
	return NomsKind(r.readUint8())
}

func (r *valueDecoder) readRef(t *Type) Ref {
	h := r.readHash()
	height := r.readUint64()
	return constructRef(t, h, height)
}

func (r *valueDecoder) readType() *Type {
	k := r.readKind()
	switch k {
	case ListKind:
		return r.tc.getCompoundType(ListKind, r.readType())
	case MapKind:
		return r.tc.getCompoundType(MapKind, r.readType(), r.readType())
	case RefKind:
		return r.tc.getCompoundType(RefKind, r.readType())
	case SetKind:
		return r.tc.getCompoundType(SetKind, r.readType())
	case StructKind:
		return r.readStructType()
	case UnionKind:
		return r.readUnionType()
	case CycleKind:
		return r.tc.getCycleType(r.readUint32())
	}

	d.Chk.True(IsPrimitiveKind(k))
	return MakePrimitiveType(k)
}

func (r *valueDecoder) readBlobLeafSequence() indexedSequence {
	b := r.readBytes()
	return newBlobLeafSequence(r.vr, b)
}

func (r *valueDecoder) readValueSequence() ValueSlice {
	count := r.readUint32()

	data := ValueSlice{}
	for i := uint32(0); i < count; i++ {
		v := r.readValue()
		data = append(data, v)
	}

	return data
}

func (r *valueDecoder) readListLeafSequence(t *Type) indexedSequence {
	data := r.readValueSequence()
	return listLeafSequence{data, t, r.vr}
}

func (r *valueDecoder) readSetLeafSequence(t *Type) orderedSequence {
	data := r.readValueSequence()
	return setLeafSequence{data, t, r.vr}
}

func (r *valueDecoder) readMapLeafSequence(t *Type) orderedSequence {
	count := r.readUint32()
	data := []mapEntry{}
	for i := uint32(0); i < count; i++ {
		k := r.readValue()
		v := r.readValue()
		data = append(data, mapEntry{k, v})
	}

	return mapLeafSequence{data, t, r.vr}
}

func (r *valueDecoder) readMetaSequence() metaSequenceData {
	count := r.readUint32()

	data := metaSequenceData{}
	for i := uint32(0); i < count; i++ {
		ref := r.readValue().(Ref)
		v := r.readValue()
		var key orderedKey
		if r, ok := v.(Ref); ok {
			// See https://github.com/attic-labs/noms/issues/1688#issuecomment-227528987
			key = orderedKeyFromHash(r.TargetHash())
		} else {
			key = newOrderedKey(v)
		}
		numLeaves := r.readUint64()
		data = append(data, newMetaTuple(ref, key, numLeaves, nil))
	}

	return data
}

func (r *valueDecoder) readIndexedMetaSequence(t *Type) indexedMetaSequence {
	return newIndexedMetaSequence(r.readMetaSequence(), t, r.vr)
}

func (r *valueDecoder) readOrderedMetaSequence(t *Type) orderedMetaSequence {
	return newOrderedMetaSequence(r.readMetaSequence(), t, r.vr)
}

func (r *valueDecoder) readValue() Value {
	t := r.readType()
	switch t.Kind() {
	case BlobKind:
		isMeta := r.readBool()
		if isMeta {
			return newBlob(r.readIndexedMetaSequence(t))
		}

		return newBlob(r.readBlobLeafSequence())
	case BoolKind:
		return Bool(r.readBool())
	case NumberKind:
		return r.readNumber()
	case StringKind:
		return String(r.readString())
	case ListKind:
		isMeta := r.readBool()
		if isMeta {
			return newList(r.readIndexedMetaSequence(t))
		}

		return newList(r.readListLeafSequence(t))
	case MapKind:
		isMeta := r.readBool()
		if isMeta {
			return newMap(r.readOrderedMetaSequence(t))
		}

		return newMap(r.readMapLeafSequence(t))
	case RefKind:
		return r.readRef(t)
	case SetKind:
		isMeta := r.readBool()
		if isMeta {
			return newSet(r.readOrderedMetaSequence(t))
		}

		return newSet(r.readSetLeafSequence(t))
	case StructKind:
		return r.readStruct(t)
	case TypeKind:
		return r.readType()
	case CycleKind, UnionKind, ValueKind:
		d.Chk.Fail(fmt.Sprintf("A value instance can never have type %s", KindToString[t.Kind()]))
	}

	panic("not reachable")
}

func (r *valueDecoder) readStruct(t *Type) Value {
	// We've read `[StructKind, name, fields, unions` at this point
	desc := t.Desc.(StructDesc)
	count := desc.Len()
	values := make([]Value, count)
	for i := 0; i < count; i++ {
		values[i] = r.readValue()
	}

	return Struct{values, t, &hash.Hash{}}
}

func (r *valueDecoder) readCachedStructType() *Type {
	trie := r.tc.trieRoots[StructKind].Traverse(r.readIdent(r.tc))
	count := r.readUint32()

	for i := uint32(0); i < count; i++ {
		trie = trie.Traverse(r.readIdent(r.tc))
		trie = trie.Traverse(r.readType().id)
	}

	return trie.t
}

func (r *valueDecoder) readStructType() *Type {
	// Try to decode cached type without allocating
	pos := r.pos()
	t := r.readCachedStructType()
	if t != nil {
		return t
	}

	// Cache miss. Go back to read and create type
	r.seek(pos)

	name := r.readString()
	count := r.readUint32()

	fieldNames := make([]string, count)
	fieldTypes := make([]*Type, count)
	for i := uint32(0); i < count; i++ {
		fieldNames[i] = r.readString()
		fieldTypes[i] = r.readType()
	}

	return r.tc.makeStructType(name, fieldNames, fieldTypes)
}

func (r *valueDecoder) readUnionType() *Type {
	l := r.readUint32()
	ts := make(typeSlice, l)
	for i := uint32(0); i < l; i++ {
		ts[i] = r.readType()
	}
	return r.tc.getCompoundType(UnionKind, ts...)
}
