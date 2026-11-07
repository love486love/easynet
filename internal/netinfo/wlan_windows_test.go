//go:build windows

package netinfo

import (
	"encoding/xml"
	"fmt"
	"testing"
)

func TestOpenProfileXMLIsValid(t *testing.T) {
	raw := fmt.Sprintf(openProfileTemplate, "JXAU-Stu", "JXAU-Stu")

	var v struct {
		XMLName xml.Name `xml:"WLANProfile"`
		Name    string   `xml:"name"`
		SSID    struct {
			Name string `xml:"name"`
		} `xml:"SSIDConfig>SSID"`
		Auth struct {
			Mode string `xml:"authentication"`
		} `xml:"MSM>security>authEncryption"`
	}
	if err := xml.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("profile XML 无法解析: %v", err)
	}
	if v.Name != "JXAU-Stu" || v.SSID.Name != "JXAU-Stu" {
		t.Fatalf("profile 名称不符: %+v", v)
	}
	if v.Auth.Mode != "open" {
		t.Fatalf("认证方式应为 open，实际 %q", v.Auth.Mode)
	}
}

func TestEscapeXML(t *testing.T) {
	if got := escapeXML("a&b<c>d"); got != "a&amp;b&lt;c&gt;d" {
		t.Fatalf("转义结果不符: %q", got)
	}
}
