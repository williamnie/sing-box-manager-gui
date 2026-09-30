package migration

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestGenericConfigSourcesPreservePolicies(t *testing.T) {
	config := `{"outbounds":[{"type":"socks","tag":"office-proxy","server":"proxy.example","server_port":1080},{"type":"selector","tag":"chosen","outbounds":["office-proxy"],"default":"office-proxy"},{"type":"direct","tag":"local"}],"route":{"final":"chosen","rules":[{"domain_suffix":["baidu.com"],"outbound":"local"},{"source_ip_cidr":["192.0.2.20/32"],"protocol":["stun"],"outbound":"chosen"},{"rule_set":["private-networks"],"outbound":"local"}],"rule_set":[{"type":"inline","tag":"private-networks","rules":[{"ip_is_private":true}]}]}}`
	var original struct {
		Outbounds []map[string]any `json:"outbounds"`
		Route     struct {
			Rules    []map[string]any `json:"rules"`
			RuleSets []map[string]any `json:"rule_set"`
		} `json:"route"`
	}
	if err := json.Unmarshal([]byte(config), &original); err != nil {
		t.Fatal(err)
	}
	wrappedText, err := json.Marshal(map[string]any{"config": config, "exporter": "example"})
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"native":         config,
		"object wrapper": `{"config":` + config + `,"exporter":"example"}`,
		"string wrapper": string(wrappedText),
	} {
		t.Run(name, func(t *testing.T) {
			report, err := Preview(input)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Blockers) != 0 {
				t.Fatal(report.Blockers)
			}
			p := report.Policy
			if !reflect.DeepEqual(p.Outbounds[:len(original.Outbounds)], original.Outbounds) {
				t.Fatal("outbound order, tags or selector references changed")
			}
			if !reflect.DeepEqual(p.Rules, original.Route.Rules) || !reflect.DeepEqual(p.RuleSets, original.Route.RuleSets) || p.Final != "chosen" {
				t.Fatal("route order or references changed")
			}
			if p.SourceHash == "" || p.SourceHash != report.Hash {
				t.Fatal("preview hash lost")
			}
		})
	}
}

func TestLegacyExportPreservesUserTagsAndSourceScope(t *testing.T) {
	report, err := Preview(`{"config":{"outbounds":[{"type":"socks","tag":"SMbox/node","server":"proxy.example","server_port":1080},{"type":"selector","tag":"SMbox/Proxy","outbounds":["SMbox/node"],"default":"SMbox/node"}],"route":{"final":"SMbox/Proxy","rules":[{"source_ips":["192.0.2.20/32"],"protocol":["stun"],"outbound":"SMbox/Proxy"},{"domain_suffix":["example.cn"],"outbound":"DIRECT"}]}}}`)
	if err != nil || len(report.Blockers) != 0 {
		t.Fatalf("legacy export rejected: %v, %#v", err, report)
	}
	p := report.Policy
	if p.Outbounds[0]["tag"] != "SMbox/node" || p.Outbounds[1]["tag"] != "SMbox/Proxy" || p.Outbounds[1]["default"] != "SMbox/node" || p.Final != "SMbox/Proxy" {
		t.Fatal("user-owned tags renamed")
	}
	if !reflect.DeepEqual(p.Outbounds[1]["outbounds"], []any{"SMbox/node"}) || p.Rules[0]["outbound"] != "SMbox/Proxy" {
		t.Fatal("legacy outbound references changed")
	}
	if !reflect.DeepEqual(p.Rules[0]["source_ip_cidr"], []any{"192.0.2.20/32"}) || p.Rules[0]["source_ips"] != nil {
		t.Fatal("legacy source scope not adapted")
	}
	if p.Rules[1]["outbound"] != "DIRECT" || !reflect.DeepEqual(p.Rules[1]["domain_suffix"], []any{"example.cn"}) {
		t.Fatal("original rule order changed")
	}
}

func TestRuntimeFieldsAreReportedAndNeverImported(t *testing.T) {
	input := `{"outbounds":[{"type":"socks","tag":"Proxy","server":"proxy.example","server_port":1080}],"route":{"auto_detect_interface":true,"default_interface":"old-uplink","default_domain_resolver":"old-dns","default_mark":123,"find_process":true},"inbounds":[{"type":"tproxy","listen_port":12345}],"experimental":{"clash_api":{"external_controller":"0.0.0.0:12346"}},"log":{"output":"/old-host/log"},"ntp":{"enabled":true,"server":"time.example"}}`
	report, err := Preview(input)
	if err != nil || len(report.Blockers) != 0 {
		t.Fatalf("runtime settings should be reported as omitted: %v, %#v", err, report)
	}
	if len(report.Omitted) != 9 || !sort.StringsAreSorted(report.Omitted) {
		t.Fatalf("incomplete or unstable omitted report: %v", report.Omitted)
	}
	data, err := json.Marshal(report.Policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"inbounds", "experimental", "log", "ntp", "auto_detect_interface", "default_interface", "default_domain_resolver", "default_mark", "find_process", "old-uplink", "old-host"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Fatalf("runtime field %s was imported", field)
		}
	}
}

func TestImportPreservesNodesSelectorsRuleOrderAndDNS(t *testing.T) {
	input := `{"outbounds":[{"type":"socks","tag":"node","server":"example.org","server_port":1080,"password":"private"},{"type":"selector","tag":"Proxy","outbounds":["node"],"default":"node"},{"type":"direct","tag":"DIRECT"}],"route":{"final":"Proxy","rules":[{"source_ips":["192.0.2.205"],"network":"udp","port_range":["6881:60000"],"outbound":"DIRECT"},{"domain_suffix":["example.org"],"outbound":"Proxy"}]},"dns":{"servers":[{"type":"hosts","tag":"dns_hosts","predefined":{"nas.lan":"192.0.2.3"}},{"type":"udp","tag":"dns_direct","server":"9.9.9.9"}],"rules":[{"domain":"nas.lan","server":"dns_hosts"}],"final":"dns_direct"},"inbounds":[{"type":"tproxy","listen_port":9888}]}`
	r, e := Preview(input)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Blockers) > 0 {
		t.Fatal(r.Blockers)
	}
	if r.Policy.Rules[0]["source_ip_cidr"] == nil || r.Policy.Rules[0]["port_range"] == nil {
		t.Fatal("lost match")
	}
	if r.Policy.Outbounds[0]["password"] != "private" {
		t.Fatal("lost outbound secret")
	}
	b, _ := json.Marshal(r.Policy.DNS)
	if !strings.Contains(string(b), "imported-dns_hosts") {
		t.Fatal("DNS tags not isolated")
	}
	if len(r.Omitted) == 0 {
		t.Fatal("silent inbound loss")
	}
}
func TestImportBlocksUnportableAndUnknownFields(t *testing.T) {
	for _, input := range []string{`{"outbounds":[{"type":"direct","tag":"DIRECT"}],"endpoints":[{"type":"wireguard"}]}`, `{"outbounds":[{"type":"socks","tag":"Proxy","server":"a","server_port":1}],"route":{"rules":[{"inbound":"old","outbound":"Proxy"}]}}`} {
		r, e := Preview(input)
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Blockers) == 0 {
			t.Fatal("unknown semantics silently lost")
		}
	}
}

func TestMalformedFieldsFailWithoutPanic(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{"config":null}`, `{"config":"null"}`, `{"config":[]}`, `{"config":"[]"}`, `{"outbounds":[{"type":"socks"}]}`, `{"outbounds":[{"type":"socks","tag":"Proxy"}],"route":{"rules":"invalid"}}`, `{"outbounds":[{"type":"socks","tag":"Proxy"}],"dns":[]}`} {
		if _, e := Preview(input); e == nil {
			t.Fatal("malformed input accepted")
		}
	}
}

func TestLegacySourceConflictRemainsBlocked(t *testing.T) {
	report, err := Preview(`{"outbounds":[{"type":"socks","tag":"Proxy"}],"route":{"rules":[{"source_ips":["192.0.2.20/32"],"source_ip_cidr":["192.0.2.21/32"],"outbound":"Proxy"}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Blockers) == 0 {
		t.Fatal("conflicting source scopes silently accepted")
	}
}
func TestResolveRulesFollowRenamedDNSServer(t *testing.T) {
	r, e := Preview(`{"outbounds":[{"type":"socks","tag":"Proxy","server":"example.org","server_port":1080}],"route":{"rules":[{"action":"resolve","server":"dns_direct"}]},"dns":{"servers":[{"type":"udp","tag":"dns_direct","server":"9.9.9.9"}],"final":"dns_direct"}}`)
	if e != nil {
		t.Fatal(e)
	}
	if r.Policy.Rules[0]["server"] != "imported-dns_direct" {
		t.Fatal("resolve silently changed upstream")
	}
}
