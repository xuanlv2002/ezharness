package config

import "testing"

/* 监听锁定仅本机：ListenAddr 恒 127.0.0.1，这是三分架构的边界承诺。 */
func TestListenAddrAlwaysLocalhost(t *testing.T) {
	if got := ListenAddr(8080); got != "127.0.0.1:8080" {
		t.Fatalf("ListenAddr(8080) = %q, want 127.0.0.1:8080", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	c := defaultConfig()
	if c.Port != DefaultPort {
		t.Fatalf("default port = %d, want %d", c.Port, DefaultPort)
	}
}
