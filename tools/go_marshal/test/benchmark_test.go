// Copyright 2019 The gVisor Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package benchmark_test

import (
	"bytes"
	encbin "encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"gvisor.dev/gvisor/pkg/binary"
	"gvisor.dev/gvisor/tools/go_marshal/analysis"
	test "gvisor.dev/gvisor/tools/go_marshal/test"
)

var order encbin.ByteOrder = encbin.LittleEndian

// Marshalling using the standard encoding/binary package.
func BenchmarkEncodingBinary(b *testing.B) {
	var s1, s2 test.Stat
	analysis.RandomizeValue(&s1)

	size := encbin.Size(&s1)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		buf := bytes.NewBuffer(make([]byte, size))
		buf.Reset()
		if err := encbin.Write(buf, order, &s1); err != nil {
			b.Error("Write:", err)
		}
		if err := encbin.Read(buf, order, &s2); err != nil {
			b.Error("Read:", err)
		}
	}

	b.StopTimer()

	// Sanity check, make sure the values were preserved.
	if !reflect.DeepEqual(s1, s2) {
		panic(fmt.Sprintf("Data corruption across marshal/unmarshal cycle:\nBefore: %+v\nAfter: %+v\n", s1, s2))
	}
}

// Marshalling using the sentry's binary.Marshal.
func BenchmarkBinary(b *testing.B) {
	var s1, s2 test.Stat
	analysis.RandomizeValue(&s1)

	size := binary.Size(s1)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		buf := make([]byte, 0, size)
		buf = binary.Marshal(buf, order, &s1)
		binary.Unmarshal(buf, order, &s2)
	}

	b.StopTimer()

	// Sanity check, make sure the values were preserved.
	if !reflect.DeepEqual(s1, s2) {
		panic(fmt.Sprintf("Data corruption across marshal/unmarshal cycle:\nBefore: %+v\nAfter: %+v\n", s1, s2))
	}
}

// Marshalling field-by-field with manually-written code.
func BenchmarkMarshalManual(b *testing.B) {
	var s1, s2 test.Stat
	analysis.RandomizeValue(&s1)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		buf := make([]byte, 0, s1.SizeBytes())

		// Marshal
		buf = binary.AppendUint64(buf, order, s1.Dev)
		buf = binary.AppendUint64(buf, order, s1.Ino)
		buf = binary.AppendUint64(buf, order, s1.Nlink)
		buf = binary.AppendUint32(buf, order, s1.Mode)
		buf = binary.AppendUint32(buf, order, s1.UID)
		buf = binary.AppendUint32(buf, order, s1.GID)
		buf = binary.AppendUint32(buf, order, 0)
		buf = binary.AppendUint64(buf, order, s1.Rdev)
		buf = binary.AppendUint64(buf, order, uint64(s1.Size))
		buf = binary.AppendUint64(buf, order, uint64(s1.Blksize))
		buf = binary.AppendUint64(buf, order, uint64(s1.Blocks))
		buf = binary.AppendUint64(buf, order, uint64(s1.ATime.Sec))
		buf = binary.AppendUint64(buf, order, uint64(s1.ATime.Nsec))
		buf = binary.AppendUint64(buf, order, uint64(s1.MTime.Sec))
		buf = binary.AppendUint64(buf, order, uint64(s1.MTime.Nsec))
		buf = binary.AppendUint64(buf, order, uint64(s1.CTime.Sec))
		buf = binary.AppendUint64(buf, order, uint64(s1.CTime.Nsec))

		// Unmarshal
		s2.Dev = order.Uint64(buf[0:8])
		s2.Ino = order.Uint64(buf[8:16])
		s2.Nlink = order.Uint64(buf[16:24])
		s2.Mode = order.Uint32(buf[24:28])
		s2.UID = order.Uint32(buf[28:32])
		s2.GID = order.Uint32(buf[32:36])
		// Padding: buf[36:40]
		s2.Rdev = order.Uint64(buf[40:48])
		s2.Size = int64(order.Uint64(buf[48:56]))
		s2.Blksize = int64(order.Uint64(buf[56:64]))
		s2.Blocks = int64(order.Uint64(buf[64:72]))
		s2.ATime.Sec = int64(order.Uint64(buf[72:80]))
		s2.ATime.Nsec = int64(order.Uint64(buf[80:88]))
		s2.MTime.Sec = int64(order.Uint64(buf[88:96]))
		s2.MTime.Nsec = int64(order.Uint64(buf[96:104]))
		s2.CTime.Sec = int64(order.Uint64(buf[104:112]))
		s2.CTime.Nsec = int64(order.Uint64(buf[112:120]))
	}

	b.StopTimer()

	// Sanity check, make sure the values were preserved.
	if !reflect.DeepEqual(s1, s2) {
		panic(fmt.Sprintf("Data corruption across marshal/unmarshal cycle:\nBefore: %+v\nAfter: %+v\n", s1, s2))
	}
}

// Marshalling with the go_marshal safe API.
func BenchmarkGoMarshalSafe(b *testing.B) {
	var s1, s2 test.Stat
	analysis.RandomizeValue(&s1)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		buf := make([]byte, s1.SizeBytes())
		s1.MarshalBytes(order, buf)
		s2.UnmarshalBytes(order, buf)
	}

	b.StopTimer()

	// Sanity check, make sure the values were preserved.
	if !reflect.DeepEqual(s1, s2) {
		panic(fmt.Sprintf("Data corruption across marshal/unmarshal cycle:\nBefore: %+v\nAfter: %+v\n", s1, s2))
	}
}

// Marshalling with the go_marshal unsafe API.
func BenchmarkGoMarshalUnsafe(b *testing.B) {
	var s1, s2 test.Stat
	analysis.RandomizeValue(&s1)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		buf := make([]byte, s1.SizeBytes())
		s1.MarshalUnsafe(order, buf)
		s2.UnmarshalUnsafe(order, buf)
	}

	b.StopTimer()

	// Sanity check, make sure the values were preserved.
	if !reflect.DeepEqual(s1, s2) {
		panic(fmt.Sprintf("Data corruption across marshal/unmarshal cycle:\nBefore: %+v\nAfter: %+v\n", s1, s2))
	}
}
