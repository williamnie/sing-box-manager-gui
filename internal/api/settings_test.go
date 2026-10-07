package api

import (
	"encoding/json"
	"testing"

	"github.com/xiaobei/singbox-manager/internal/storage"
)

// 新装实例应能直接配置 mixed 代理，不需要先重启管理器或保存网关草案。
func TestFreshInstallCanSaveDesktopProxySettings(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			s := testServer(t)
			s.platform = platform
			cookie := setup(t, s)
			response := request(s, "GET", "/api/settings", nil, cookie, "")
			var body struct {
				Data storage.Settings `json:"data"`
			}
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &body) != nil {
				t.Fatal("读取首次设置失败", response.Body.String())
			}
			body.Data.AutoApply = false
			body.Data.TunEnabled = false
			body.Data.AllowLAN = true
			body.Data.MixedPort = 22080
			response = request(s, "PUT", "/api/settings", body.Data, cookie, "")
			if response.Code != 200 {
				t.Fatal("新装实例的普通代理设置被拒绝", response.Code, response.Body.String())
			}
			reopened, err := storage.NewJSONStore(s.store.GetDataDir())
			if err != nil {
				t.Fatal(err)
			}
			for _, settings := range []*storage.Settings{s.store.GetSettings(), reopened.GetSettings()} {
				if settings.TunEnabled || !settings.AllowLAN || settings.MixedPort != 22080 || settings.DeploymentRole != "desktop" || settings.Gateway.Enabled {
					t.Fatal("设置未保存，或意外启用了网关")
				}
			}
		})
	}
}
