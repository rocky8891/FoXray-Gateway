package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/rocky8891/FoXray-Gateway/backend/internal/gateway"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	managerUpdate := flag.Bool("apply-manager-update", false, "apply a verified manager release without changing component services")
	warpProbe := flag.String("warp-probe", "", "internal isolated WARP measurement")
	wgPeer := flag.String("wireguard-peer", "", "synchronize one peer from its saved enabled state")
	wgNetwork := flag.String("wireguard-network", "", "apply interface-scoped network rules: up or down")
	wgInterface := flag.String("interface", "", "WireGuard interface")
	base := flag.String("base", "/opt/foxray", "runtime directory")
	listen := flag.String("listen", "0.0.0.0:18880", "HTTP address")
	ui := flag.String("ui", "/opt/foxray/web", "React assets")
	checkMigration := flag.Bool("check-migration", false, "validate existing forwarding before migration")
	readonly := flag.Bool("read-only", false, "disable changes and periodic jobs")
	rollback := flag.String("rollback-plan", "", "execute an armed network rollback")
	vpnGuard := flag.Bool("vpn-guard", false, "refresh existing VPN bypass")
	warpNetwork := flag.Bool("warp-network", false, "initialize isolated WARP namespace")
	ensureModule := flag.String("ensure-module", "", "detect, adopt or install xray, warp, wireguard")
	acceptTerms := flag.Bool("accept-warp-tos", false, "accept Cloudflare WARP terms for first install")
	komariCheck := flag.String("check-komari-url", "", "validate an independently installed Komari URL")
	komariNormalize := flag.String("normalize-komari-url", "", "validate Komari URL without connecting")
	initialize := flag.Bool("initialize", false, "initialize optional component selection")
	withXray := flag.Bool("with-xray", true, "select Xray module")
	withoutXray := flag.Bool("without-xray", false, "disable Xray module")
	withWireguard := flag.Bool("with-wireguard", false, "select WireGuard module")
	withWarp := flag.Bool("with-warp", false, "select WARP component")
	withKomari := flag.Bool("with-komari", false, "select Komari component")
	checkTopology := flag.Bool("check-topology", false, "inspect and validate network topology without applying changes")
	stageBackup := flag.String("stage-config-backup", "", "decrypt a FoXray configuration backup into a new private directory")
	backupIdentity := flag.String("backup-identity", "", "RSA private key file for offline restoration")
	backupDest := flag.String("backup-dest", "", "new directory for staged restoration")
	flag.Parse()
	if *stageBackup != "" {
		if *backupDest == "" {
			log.Fatal("请指定 --backup-dest")
		}
		if err := gateway.StageConfigBackup(*stageBackup, *backupIdentity, *backupDest, readBackupPassword); err != nil {
			log.Fatal(err)
		}
		log.Print("备份已校验并解包到指定目录")
		return
	}
	if *checkTopology {
		topology, err := gateway.CheckTopology()
		if err != nil {
			log.Fatal(err)
		}
		_ = json.NewEncoder(os.Stdout).Encode(topology)
		return
	}
	if *managerUpdate {
		if *readonly || *checkMigration {
			log.Print("只读或迁移检查模式不允许应用版本更新")
			os.Exit(1)
		}
		if err := gateway.ApplyManagerUpdate(*base); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if *warpProbe != "" {
		if *warpProbe != "latency" && *warpProbe != "speed" {
			os.Exit(1)
		}
		p := gateway.M{}
		if json.NewDecoder(os.Stdin).Decode(&p) != nil {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(gateway.RunWarpProbe(*warpProbe, p))
		return
	}
	if *komariNormalize != "" {
		address, e := gateway.NormalizeKomariURL(*komariNormalize)
		if e != nil {
			log.Print(e)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"url": address})
		return
	}
	if *komariCheck != "" {
		data, err := gateway.CheckKomari(*komariCheck)
		if err != nil {
			log.Print(err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(data)
		return
	}
	if *ensureModule != "" {
		if *readonly || *checkMigration {
			log.Print("只读或迁移检查模式不允许部署模块")
			os.Exit(1)
		}
		if err := gateway.EnsureModule(*base, *ensureModule, *acceptTerms); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if *wgPeer != "" {
		if *readonly || *checkMigration {
			os.Exit(1)
		}
		if err := gateway.WireGuardPeerState(*wgInterface, *wgPeer); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		return
	}
	if *wgNetwork != "" {
		if e := gateway.WireGuardNetwork(*wgNetwork, *wgInterface); e != nil {
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if *vpnGuard {
		if e := gateway.VPNGuard(true); e != nil {
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if *warpNetwork {
		if e := gateway.SetupWarpNetwork(); e != nil {
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if *initialize {
		if e := gateway.InitializeModules(*base, *withXray && !*withoutXray, *withWarp, *withWireguard, *withKomari); e != nil {
			log.Print(e)
			os.Exit(1)
		}
		return
	}
	if *rollback != "" {
		if e := gateway.RollbackFile(*rollback); e != nil {
			log.Print("网络回退未完成：", e)
			os.Exit(1)
		}
		return
	}
	app, err := gateway.New(*base, *ui, *readonly || *checkMigration)
	if err != nil {
		log.Fatal("初始化失败：", err)
	}
	defer app.Close()
	if *checkMigration {
		_ = json.NewEncoder(os.Stdout).Encode(app.MigrationReport())
		return
	}
	app.ConfigureListener(*listen)
	security, err := gateway.NewManagementSecurity(app, *listen)
	if err != nil {
		log.Fatal("无法加载管理登录：", err)
	}
	defer security.Close()
	app.Start()
	srv := &http.Server{Addr: *listen, Handler: security, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	// Reuse the UI on loopback for SSH-forwarded backup exports even when LAN binds to a specific IPv4 address.
	var loopback *http.Server
	if host, port, splitErr := net.SplitHostPort(*listen); splitErr == nil && !*readonly {
		if ip := net.ParseIP(host); ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() {
			if local, listenErr := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", port)); listenErr == nil {
				loopback = &http.Server{Handler: security, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
				go func() {
					if e := loopback.Serve(local); e != nil && e != http.ErrServerClosed {
						log.Print("本机备份监听停止：", e)
					}
				}()
			} else {
				log.Print("无法启用本机备份监听：", listenErr)
			}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if loopback != nil {
			_ = loopback.Close()
		}
		_ = srv.Shutdown(c)
	}()
	log.Printf("FoXray Gateway %s HTTP=%s read-only=%v", gateway.Version, *listen, *readonly)
	if err = srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Print("服务停止：", err)
		os.Exit(1)
	}
}

func readBackupPassword() (string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		stty := func(arg string) error {
			cmd := exec.Command("stty", arg)
			cmd.Stdin = os.Stdin
			return cmd.Run()
		}
		if err := stty("-echo"); err != nil {
			return "", fmt.Errorf("无法关闭终端回显：%w", err)
		}
		defer func() { _ = stty("echo"); fmt.Fprintln(os.Stderr) }()
		fmt.Fprint(os.Stderr, "请输入备份密码：")
	}
	line, err := bufio.NewReader(io.LimitReader(os.Stdin, 1026)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(password) == 0 || len(password) > 1024 {
		return "", fmt.Errorf("请输入有效的备份密码")
	}
	return password, nil
}
