package node

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestKeybytesToHex(t *testing.T) {
	type args struct {
		str []byte
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "Test 1",
			args: args{
				str: common.FromHex("1111"),
			},
			want: common.FromHex("0101010110"),
		},
		{
			name: "Test empty string",
			args: args{
				str: []byte{},
			},
			want: []byte{16},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KeybytesToHex(tt.args.str); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("KeybytesToHex() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPrefixLen(t *testing.T) {
	type args struct {
		a []byte
		b []byte
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "Test 1",
			args: args{
				a: common.FromHex("01010302"),
				b: common.FromHex("01010202"),
			},
			want: 2,
		},
		{
			name: "Test no common prefix",
			args: args{
				a: common.FromHex("01010302"),
				b: common.FromHex("02010302"),
			},
			want: 0,
		},
		{
			name: "Test all common prefix",
			args: args{
				a: common.FromHex("01010302"),
				b: common.FromHex("01010302"),
			},
			want: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrefixLen(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("PrefixLen() = %v, want %v", got, tt.want)
			}
		})
	}
}
