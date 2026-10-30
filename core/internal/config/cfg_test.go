package config

import "testing"

/*
首次运行新建配置必须带回归一后的监听地址：否则设置页在第一次启动时

	显示为空（配置文件落盘后第二次启动才恢复）。
*/
func TestDefaultConfigListen(t *testing.T) {
	c := defaultConfig()
	if c.Listen != "127.0.0.1" {
		t.Fatalf("default listen = %q, want 127.0.0.1", c.Listen)
	}
	if c.Port != DefaultPort {
		t.Fatalf("default port = %d, want %d", c.Port, DefaultPort)
	}
}
