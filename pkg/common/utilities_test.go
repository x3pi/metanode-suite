package common

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestGetRealConnectionAddress(t *testing.T) {
	type args struct {
		dnsLink string
		address common.Address
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "valid_address_but_no_server_running_returns_error",
			args: args{
				dnsLink: "http://127.0.0.1:9999/api/dns/connection-address/",
				address: common.HexToAddress("51bdebc98ad4e158b7bc02220ab8ab4cf18af6bd"),
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetRealConnectionAddress(tt.args.dnsLink, tt.args.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRealConnectionAddress() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetRealConnectionAddress() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadLastLine(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.csv")
	err := os.WriteFile(tempFile, []byte("line1\nline2\nline3\nlast_line"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	type args struct {
		filePath string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name: "read_existing_file",
			args: args{
				filePath: tempFile,
			},
			want:    "last_line",
			wantErr: false,
		},
		{
			name: "read_non_existing_file",
			args: args{
				filePath: filepath.Join(tempDir, "does_not_exist.csv"),
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadLastLine(tt.args.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadLastLine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ReadLastLine() = %v, want %v", got, tt.want)
			}
		})
	}
}
