package ctl

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parse 允许参数与位置参数交错，便于 Agent 按任意顺序书写。
func parse(e env, name string, args []string, define func(*flag.FlagSet)) ([]string, bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	define(fs)
	positional := []string{}
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			fmt.Fprintf(e.stderr, "sbm ctl %s: %v\n", name, err)
			return nil, false
		}
		args = fs.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, true
}

func usageErr(e env, msg string) int {
	fmt.Fprintln(e.stderr, "sbm ctl: "+msg)
	fmt.Fprintln(e.stderr, "运行 sbm ctl help 查看用法")
	return ExitUsage
}

func dispatch(c *client, args []string, e env) int {
	cmd, rest := args[0], args[1:]
	get := func(path string, q url.Values) int {
		resp, err := c.do("GET", path, q, nil, "")
		return emit(e, resp, err)
	}
	switch cmd {
	case "capabilities", "status", "kernel", "proxies", "connections":
		path := map[string]string{"capabilities": "/capabilities", "status": "/status", "kernel": "/kernel", "proxies": "/runtime/proxies", "connections": "/runtime/connections"}[cmd]
		return get(path, nil)
	case "diff":
		return get("/config/diff", nil)
	case "versions":
		return get("/config/versions", nil)
	case "rules":
		return get("/rules/overview", nil)
	case "audit":
		var limit int
		if _, ok := parse(e, cmd, rest, func(fs *flag.FlagSet) { fs.IntVar(&limit, "limit", 100, "") }); !ok {
			return ExitUsage
		}
		return get("/audit", url.Values{"limit": {strconv.Itoa(limit)}})
	case "get":
		if len(rest) != 1 {
			return usageErr(e, "get 需要资源名")
		}
		return get("/resources/"+url.PathEscape(rest[0]), nil)
	case "nodes":
		var country string
		if _, ok := parse(e, cmd, rest, func(fs *flag.FlagSet) { fs.StringVar(&country, "country", "", "") }); !ok {
			return ExitUsage
		}
		return get("/nodes", url.Values{"country": {country}})
	case "gateway":
		if len(rest) != 1 || (rest[0] != "status" && rest[0] != "clients" && rest[0] != "plan") {
			return usageErr(e, "gateway 需要 status、clients 或 plan")
		}
		return get("/gateway/"+rest[0], nil)
	case "explain":
		return explain(c, rest, e)
	case "logs":
		var source, grep string
		var lines int
		if _, ok := parse(e, cmd, rest, func(fs *flag.FlagSet) {
			fs.StringVar(&source, "source", "singbox", "")
			fs.StringVar(&grep, "grep", "", "")
			fs.IntVar(&lines, "lines", 200, "")
		}); !ok {
			return ExitUsage
		}
		return get("/logs", url.Values{"source": {source}, "grep": {grep}, "lines": {strconv.Itoa(lines)}})
	case "dns":
		var domain, clientIP string
		var limit int
		if _, ok := parse(e, cmd, rest, func(fs *flag.FlagSet) {
			fs.StringVar(&domain, "domain", "", "")
			fs.StringVar(&clientIP, "client", "", "")
			fs.IntVar(&limit, "limit", 50, "")
		}); !ok {
			return ExitUsage
		}
		q := url.Values{"limit": {strconv.Itoa(limit)}, "sort": {"recent"}}
		if domain != "" {
			q.Set("search", domain)
		}
		if clientIP != "" {
			q.Set("source", clientIP)
		}
		return get("/dns/queries", q)
	case "select":
		return selectNode(c, rest, e)
	case "delay":
		return delay(c, rest, e)
	case "change":
		return change(c, rest, e)
	case "op":
		return genericOp(c, rest, e)
	case "rule":
		return ruleCmd(c, rest, e)
	case "block":
		if len(rest) < 2 || (rest[0] != "add" && rest[0] != "remove") {
			return usageErr(e, "block add|remove <域名...>")
		}
		value, _ := json.Marshal(map[string]any{"domains": rest[1:]})
		return createOps(c, e, "", "", []map[string]any{{"action": rest[0], "resource": "blocklist", "value": json.RawMessage(value)}}, "")
	case "sub":
		return subCmd(c, rest, e)
	case "node":
		return nodeCmd(c, rest, e)
	}
	return usageErr(e, "未知命令 "+cmd)
}

func explain(c *client, args []string, e env) int {
	if len(args) == 0 {
		return usageErr(e, "explain domain <域名> 或 explain client <IP>")
	}
	switch args[0] {
	case "domain":
		var source string
		pos, ok := parse(e, "explain domain", args[1:], func(fs *flag.FlagSet) { fs.StringVar(&source, "source", "", "") })
		if !ok || len(pos) != 1 {
			return usageErr(e, "explain domain <域名> [--source IP]")
		}
		q := url.Values{"domain": {pos[0]}}
		if source != "" {
			q.Set("source", source)
		}
		resp, err := c.do("GET", "/explain/domain", q, nil, "")
		return emit(e, resp, err)
	case "client":
		if len(args) != 2 {
			return usageErr(e, "explain client <IP>")
		}
		resp, err := c.do("GET", "/explain/client", url.Values{"ip": {args[1]}}, nil, "")
		return emit(e, resp, err)
	}
	return usageErr(e, "explain 只支持 domain 或 client")
}

// runtimeInstance 读取当前内核实例，选择与测速必须绑定实例防止误操作新实例。
func runtimeInstance(c *client) (string, *response, error) {
	resp, err := c.do("GET", "/runtime/proxies", nil, nil, "")
	if err != nil || resp.status != 200 {
		return "", resp, err
	}
	var payload struct {
		Data struct {
			Instance string `json:"instance"`
		} `json:"data"`
	}
	_ = json.Unmarshal(resp.body, &payload)
	return payload.Data.Instance, resp, nil
}

func selectNode(c *client, args []string, e env) int {
	if len(args) != 2 {
		return usageErr(e, "select <选择组> <节点>")
	}
	instance, resp, err := runtimeInstance(c)
	if instance == "" {
		return emit(e, resp, err)
	}
	resp, err = c.do("POST", "/runtime/select", nil, map[string]any{"group": args[0], "member": args[1], "instance": instance}, "")
	return emit(e, resp, err)
}

func delay(c *client, args []string, e env) int {
	var testURL string
	var timeout int
	tags, ok := parse(e, "delay", args, func(fs *flag.FlagSet) {
		fs.StringVar(&testURL, "url", "", "")
		fs.IntVar(&timeout, "timeout-ms", 5000, "")
	})
	if !ok || len(tags) == 0 {
		return usageErr(e, "delay <节点...> [--url https://...] [--timeout-ms 5000]")
	}
	instance, resp, err := runtimeInstance(c)
	if instance == "" {
		return emit(e, resp, err)
	}
	body := map[string]any{"tags": tags, "timeout": timeout, "instance": instance}
	if testURL != "" {
		body["url"] = testURL
	}
	resp, err = c.do("POST", "/runtime/delay", nil, body, "")
	return emit(e, resp, err)
}

func createOps(c *client, e env, title, includePending string, ops []map[string]any, key string) int {
	if key == "" {
		key = newKey()
	}
	body := map[string]any{"ops": ops}
	if title != "" {
		body["title"] = title
	}
	if includePending != "" {
		body["include_pending"] = includePending
	}
	resp, err := c.do("POST", "/changesets", nil, body, key)
	return emit(e, resp, err)
}

func change(c *client, args []string, e env) int {
	if len(args) == 0 {
		return usageErr(e, "change create|list|show|apply|confirm|rollback|revert")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list":
		resp, err := c.do("GET", "/changesets", nil, nil, "")
		return emit(e, resp, err)
	case "show":
		if len(rest) != 1 {
			return usageErr(e, "change show <ID>")
		}
		resp, err := c.do("GET", "/changesets/"+url.PathEscape(rest[0]), nil, nil, "")
		return emit(e, resp, err)
	case "create":
		var file, title, pending, key string
		if _, ok := parse(e, "change create", rest, func(fs *flag.FlagSet) {
			fs.StringVar(&file, "file", "", "")
			fs.StringVar(&title, "title", "", "")
			fs.StringVar(&pending, "include-pending", "", "")
			fs.StringVar(&key, "idempotency-key", "", "")
		}); !ok || file == "" {
			return usageErr(e, "change create --file ops.json|- [--title T] [--include-pending DIGEST]")
		}
		raw, err := readInput(e, file)
		if err != nil {
			return fail(e, ExitUsage, err)
		}
		// 文件可以是 {"ops":[...]} 或直接是操作数组。
		var doc struct {
			Title string           `json:"title"`
			Ops   []map[string]any `json:"ops"`
		}
		if json.Unmarshal(raw, &doc.Ops) != nil {
			if err := json.Unmarshal(raw, &doc); err != nil {
				return fail(e, ExitUsage, fmt.Errorf("ops 文件不是有效 JSON"))
			}
		}
		return createOps(c, e, firstNonEmpty(title, doc.Title), pending, doc.Ops, key)
	case "apply":
		var seconds int
		var key string
		pos, ok := parse(e, "change apply", rest, func(fs *flag.FlagSet) {
			fs.IntVar(&seconds, "confirm-within", 120, "")
			fs.StringVar(&key, "idempotency-key", "", "")
		})
		if !ok || len(pos) != 1 {
			return usageErr(e, "change apply <ID> [--confirm-within 秒]")
		}
		if key == "" {
			key = newKey()
		}
		resp, err := c.do("POST", "/changesets/"+url.PathEscape(pos[0])+"/apply", nil, map[string]any{"confirm_seconds": seconds}, key)
		return emit(e, resp, err)
	case "confirm":
		if len(rest) != 1 {
			return usageErr(e, "change confirm <ID>")
		}
		// 确认绑定本次应用的候选摘要，从服务端读取而不是让调用方手抄。
		resp, err := c.do("GET", "/changesets/"+url.PathEscape(rest[0]), nil, nil, "")
		if err != nil || resp.status != 200 {
			return emit(e, resp, err)
		}
		var payload struct {
			Data struct {
				CandidateConfig string `json:"candidate_config"`
			} `json:"data"`
		}
		_ = json.Unmarshal(resp.body, &payload)
		resp, err = c.do("POST", "/changesets/"+url.PathEscape(rest[0])+"/confirm", nil, map[string]any{"candidate_config": payload.Data.CandidateConfig}, "")
		return emit(e, resp, err)
	case "revert":
		// 撤销已确认的变更：创建一个新的撤销变更集，仍需 apply + confirm。
		if len(rest) != 1 {
			return usageErr(e, "change revert <已确认的变更集ID>")
		}
		return createOps(c, e, "撤销变更 "+rest[0], "", []map[string]any{{"action": "apply", "resource": "revert", "id": rest[0]}}, "")
	case "rollback":
		if len(rest) != 1 {
			return usageErr(e, "change rollback <ID>")
		}
		resp, err := c.do("POST", "/changesets/"+url.PathEscape(rest[0])+"/rollback", nil, map[string]any{}, "")
		return emit(e, resp, err)
	}
	return usageErr(e, "未知 change 子命令 "+sub)
}

func genericOp(c *client, args []string, e env) int {
	var inline, file, title, pending, key string
	pos, ok := parse(e, "op", args, func(fs *flag.FlagSet) {
		fs.StringVar(&inline, "value", "", "")
		fs.StringVar(&file, "value-file", "", "")
		fs.StringVar(&title, "title", "", "")
		fs.StringVar(&pending, "include-pending", "", "")
		fs.StringVar(&key, "idempotency-key", "", "")
	})
	if !ok || len(pos) < 2 || len(pos) > 3 {
		return usageErr(e, "op <action> <resource> [ID] [--value JSON | --value-file PATH|-]")
	}
	value, err := readValue(e, inline, file)
	if err != nil {
		return fail(e, ExitUsage, err)
	}
	op := map[string]any{"action": pos[0], "resource": pos[1]}
	if len(pos) == 3 {
		op["id"] = pos[2]
	}
	if value != nil {
		op["value"] = value
	}
	return createOps(c, e, title, pending, []map[string]any{op}, key)
}

func ruleCmd(c *client, args []string, e env) int {
	if len(args) == 0 {
		return usageErr(e, "rule add|delete|enable|disable")
	}
	switch args[0] {
	case "add":
		var name, ruleType, outbound, id string
		var priority int
		var values, sources multiFlag
		if _, ok := parse(e, "rule add", args[1:], func(fs *flag.FlagSet) {
			fs.StringVar(&id, "id", "", "")
			fs.StringVar(&name, "name", "", "")
			fs.StringVar(&ruleType, "type", "domain_suffix", "")
			fs.StringVar(&outbound, "outbound", "", "")
			fs.IntVar(&priority, "priority", 0, "")
			fs.Var(&values, "value", "")
			fs.Var(&sources, "source", "")
		}); !ok || name == "" || outbound == "" {
			return usageErr(e, "rule add --name N --type T --value V... --outbound O [--priority P] [--source CIDR...]")
		}
		rule := map[string]any{"name": name, "rule_type": ruleType, "values": []string(values), "outbound": outbound, "enabled": true, "priority": priority}
		if len(sources) > 0 {
			rule["source_cidrs"] = []string(sources)
		}
		value, _ := json.Marshal(rule)
		op := map[string]any{"action": "create", "resource": "rule", "value": json.RawMessage(value)}
		if id != "" {
			op["id"] = id
		}
		return createOps(c, e, "", "", []map[string]any{op}, "")
	case "delete", "enable", "disable":
		if len(args) != 2 {
			return usageErr(e, "rule "+args[0]+" <ID>")
		}
		op := map[string]any{"action": "delete", "resource": "rule", "id": args[1]}
		if args[0] != "delete" {
			op["action"], op["value"] = "update", map[string]any{"enabled": args[0] == "enable"}
		}
		return createOps(c, e, "", "", []map[string]any{op}, "")
	}
	return usageErr(e, "未知 rule 子命令 "+args[0])
}

func subCmd(c *client, args []string, e env) int {
	if len(args) == 0 {
		return usageErr(e, "sub add|refresh|delete")
	}
	switch args[0] {
	case "add":
		var name, urlFile, id string
		if _, ok := parse(e, "sub add", args[1:], func(fs *flag.FlagSet) {
			fs.StringVar(&id, "id", "", "")
			fs.StringVar(&name, "name", "", "")
			fs.StringVar(&urlFile, "url-file", "", "")
		}); !ok || name == "" || urlFile == "" {
			return usageErr(e, "sub add --name N --url-file PATH|-（订阅 URL 含凭据，不接受命令行参数）")
		}
		raw, err := readInput(e, urlFile)
		if err != nil {
			return fail(e, ExitUsage, err)
		}
		value, _ := json.Marshal(map[string]any{"name": name, "url": strings.TrimSpace(string(raw))})
		op := map[string]any{"action": "create", "resource": "subscription", "value": json.RawMessage(value)}
		if id != "" {
			op["id"] = id
		}
		return createOps(c, e, "", "", []map[string]any{op}, "")
	case "refresh", "delete":
		if len(args) != 2 {
			return usageErr(e, "sub "+args[0]+" <ID>")
		}
		return createOps(c, e, "", "", []map[string]any{{"action": args[0], "resource": "subscription", "id": args[1]}}, "")
	}
	return usageErr(e, "未知 sub 子命令 "+args[0])
}

func nodeCmd(c *client, args []string, e env) int {
	if len(args) == 0 {
		return usageErr(e, "node add|delete")
	}
	switch args[0] {
	case "add":
		var urlFile, id string
		if _, ok := parse(e, "node add", args[1:], func(fs *flag.FlagSet) {
			fs.StringVar(&id, "id", "", "")
			fs.StringVar(&urlFile, "url-file", "", "")
		}); !ok || urlFile == "" {
			return usageErr(e, "node add --url-file PATH|-（分享链接含密钥，不接受命令行参数）")
		}
		raw, err := readInput(e, urlFile)
		if err != nil {
			return fail(e, ExitUsage, err)
		}
		value, _ := json.Marshal(map[string]any{"url": strings.TrimSpace(string(raw))})
		op := map[string]any{"action": "create", "resource": "manual_node", "value": json.RawMessage(value)}
		if id != "" {
			op["id"] = id
		}
		return createOps(c, e, "", "", []map[string]any{op}, "")
	case "delete":
		if len(args) != 2 {
			return usageErr(e, "node delete <ID>")
		}
		return createOps(c, e, "", "", []map[string]any{{"action": "delete", "resource": "manual_node", "id": args[1]}}, "")
	}
	return usageErr(e, "未知 node 子命令 "+args[0])
}
