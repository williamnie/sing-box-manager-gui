// sbm-gateway is a fixed-protocol local privileged helper, never an HTTP shell.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/xiaobei/singbox-manager/internal/gateway"
)

func main() {
	var group string
	flag.StringVar(&group, "group", "singbox-manager", "Unix socket access group")
	flag.Parse()
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "sbm-gateway requires root")
		os.Exit(1)
	}
	policy, e := gateway.LoadPolicy("/etc/sbm-gateway/policy.json")
	if e != nil {
		fatal(e)
	}
	runner, e := gateway.NewSystemRunner()
	if e != nil {
		fatal(e)
	}
	g, e := user.LookupGroup(group)
	if e != nil {
		fatal(e)
	}
	gid, e := strconv.Atoi(g.Gid)
	if e != nil {
		fatal(e)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	manager := gateway.NewManager(runner, policy, "/var/lib/sbm-gateway")
	if e = gateway.Serve(ctx, manager, gateway.DefaultSocket, gid); e != nil {
		fatal(e)
	}
}
func fatal(e error) { fmt.Fprintln(os.Stderr, e); os.Exit(1) }
