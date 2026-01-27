package api

import (
	"net"
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
)

// NetworkInterface 网络接口信息
type NetworkInterface struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// getNetworkInterfaces 获取本机非回环 IPv4 地址
func (s *Server) getNetworkInterfaces(c *gin.Context) {
	interfaces, err := net.Interfaces()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "无法获取网络接口: " + err.Error()})
		return
	}

	var validInterfaces []NetworkInterface

	for _, iface := range interfaces {
		// 跳过 down 的接口和回环接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// 只保留 IPv4
			if ip == nil || ip.To4() == nil {
				continue
			}

			validInterfaces = append(validInterfaces, NetworkInterface{
				Name: iface.Name,
				IP:   ip.String(),
			})
		}
	}

	// 排序
	sort.Slice(validInterfaces, func(i, j int) bool {
		return validInterfaces[i].Name < validInterfaces[j].Name
	})

	c.JSON(http.StatusOK, gin.H{"data": validInterfaces})
}
