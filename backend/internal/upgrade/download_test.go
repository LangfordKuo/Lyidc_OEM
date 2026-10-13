package upgrade

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChecksum(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	cases := []struct {
		name      string
		body      string
		assetName string
		want      string
		wantErr   string
	}{
		{"sha256sum 格式", digest + "  lyidc-oem.zip\n", "lyidc-oem.zip", digest, ""},
		{"二进制模式前缀", digest + " *lyidc-oem.zip\n", "lyidc-oem.zip", digest, ""},
		{"纯十六进制", digest + "\n", "lyidc-oem.zip", digest, ""},
		{"大写摘要归一为小写", strings.ToUpper(digest) + "  lyidc-oem.zip\n", "lyidc-oem.zip", digest, ""},
		{"文件名不符", digest + "  other.zip\n", "lyidc-oem.zip", "", "不一致"},
		{"没有摘要", "随便写点什么\n", "lyidc-oem.zip", "", "没有 64 位十六进制摘要"},
		{"空文件", "", "lyidc-oem.zip", "", "没有 64 位十六进制摘要"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := parseChecksum(testCase.body, testCase.assetName)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("期望错误 %q，实际 %v", testCase.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("不应报错：%v", err)
			}
			if got != testCase.want {
				t.Fatalf("摘要不符：%s", got)
			}
		})
	}
}

func TestResolveDownloadURL(t *testing.T) {
	cases := []struct {
		base   string
		rawURL string
		want   string
	}{
		{"", "https://github.com/o/r/releases/download/v1/a.zip", "https://github.com/o/r/releases/download/v1/a.zip"},
		{"http://127.0.0.1:9000", "https://github.com/o/r/releases/download/v1/a.zip", "http://127.0.0.1:9000/o/r/releases/download/v1/a.zip"},
		{"http://mirror.example.com/base/", "https://github.com/o/r/releases/download/v1/a.zip", "http://mirror.example.com/base/o/r/releases/download/v1/a.zip"},
		{"http://mirror.example.com", "a.zip", "http://mirror.example.com/a.zip"},
	}
	for _, testCase := range cases {
		if got := resolveDownloadURL(testCase.base, testCase.rawURL); got != testCase.want {
			t.Fatalf("base=%q raw=%q → %q，期望 %q", testCase.base, testCase.rawURL, got, testCase.want)
		}
	}
}

func TestDownloadAssetReportsProgressAndLimits(t *testing.T) {
	body := strings.Repeat("x", 4096)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("资产下载绝不能携带 Authorization（契约 20.7 第 3 条）")
		}
		w.Header().Set("Content-Length", "4096")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "asset.zip")
	var lastDone, lastTotal int64
	written, err := downloadAsset(context.Background(), server.Client(), server.URL+"/asset.zip", target,
		int64(len(body)), func(done, total int64) { lastDone, lastTotal = done, total })
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	if written != int64(len(body)) || lastDone != int64(len(body)) || lastTotal != int64(len(body)) {
		t.Fatalf("下载字节数不符：written=%d progress=%d/%d", written, lastDone, lastTotal)
	}
	raw, err := os.ReadFile(target)
	if err != nil || len(raw) != len(body) {
		t.Fatalf("落盘内容不符：%v %d", err, len(raw))
	}
}

func TestDownloadAssetFailsOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, err := downloadAsset(context.Background(), server.Client(), server.URL+"/missing.zip",
		filepath.Join(t.TempDir(), "asset.zip"), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("HTTP 失败应报出状态码：%v", err)
	}
}

func TestFetchChecksumRejectsWrongFile(t *testing.T) {
	digest := strings.Repeat("cd", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(digest + "  another-package.zip\n"))
	}))
	defer server.Close()

	_, err := fetchChecksum(context.Background(), server.Client(), server.URL+"/a.zip.sha256", "a.zip")
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("文件名不一致应拒绝：%v", err)
	}
}

func TestFileSHA256MatchesStandardLibrary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	// echo -n hello | sha256sum
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	got, err := fileSHA256(path)
	if err != nil {
		t.Fatalf("计算摘要失败：%v", err)
	}
	if got != expected {
		t.Fatalf("摘要不符：%s", got)
	}
}
