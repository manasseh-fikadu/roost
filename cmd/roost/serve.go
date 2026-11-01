package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/manasseh-fikadu/roost/internal/agents"
	"github.com/manasseh-fikadu/roost/internal/alerts"
	"github.com/manasseh-fikadu/roost/internal/api"
	"github.com/manasseh-fikadu/roost/internal/checks"
	"github.com/manasseh-fikadu/roost/internal/config"
	"github.com/manasseh-fikadu/roost/internal/docker"
	"github.com/manasseh-fikadu/roost/internal/hostmetrics"
	"github.com/manasseh-fikadu/roost/internal/notify"
	"github.com/manasseh-fikadu/roost/internal/settings"
	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/store"
	"github.com/manasseh-fikadu/roost/internal/systemd"
	"github.com/manasseh-fikadu/roost/internal/tailnet"
	"github.com/manasseh-fikadu/roost/internal/usage"
	"github.com/manasseh-fikadu/roost/web"
)

// shutdownTimeout is how long in-flight requests get to finish after a stop signal.
const shutdownTimeout = 10 * time.Second

// openNodeFunc connects Roost to the tailnet. Tests pass a fake.
type openNodeFunc func(ctx context.Context, cfg config.Config, logger *slog.Logger) (tailnet.Node, error)

func serve(ctx context.Context, args []string, stderr io.Writer) error {
	cfg, err := config.Parse(args, os.Getenv, stderr)
	if err != nil {
		return err
	}
	return runServe(ctx, cfg, stderr, openNode)
}

// openNode starts the embedded node, or connects to the machine's tailscaled,
// as cfg says.
func openNode(ctx context.Context, cfg config.Config, logger *slog.Logger) (tailnet.Node, error) {
	if cfg.Tailscale == config.Host {
		return tailnet.NewHost(ctx, tailnet.HostOptions{Port: cfg.Port, Logger: logger})
	}
	return tailnet.NewEmbedded(tailnet.EmbeddedOptions{
		Dir:      filepath.Join(cfg.DataDir, "tailscale"),
		Hostname: cfg.Hostname,
		Logger:   logger,
	})
}

// runServe runs the hub until ctx ends. The app is only served on the tailnet.
// In embedded mode, the setup page is also served on cfg.SetupListen, so
// someone can log the node in.
func runServe(ctx context.Context, cfg config.Config, stderr io.Writer, open openNodeFunc) (err error) {
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	devUI, err := devUIURL(cfg.DevUI)
	if err != nil {
		return err
	}
	ui := web.FS()
	if ui == nil && devUI == nil {
		logger.Warn("built without the web UI, so only /api is served; `make build` embeds it")
	}

	dbPath := filepath.Join(cfg.DataDir, "roost.db")
	st, err := store.Open(ctx, dbPath, store.Options{Logger: logger, Retention: settings.StoreRetention})
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer func() { err = errors.Join(err, st.Close()) }()

	// Listen for the setup page before starting the node, so a bad address
	// fails at once.
	var setupLn net.Listener
	if cfg.Tailscale == config.Embedded {
		var lc net.ListenConfig
		if setupLn, err = lc.Listen(ctx, "tcp", cfg.SetupListen); err != nil {
			return fmt.Errorf("listen on %s: %w", cfg.SetupListen, err)
		}
		// Shutting the setup server down closes it too, and closing twice is harmless.
		defer func() { _ = setupLn.Close() }()
	}

	node, err := open(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("connect to the tailnet: %w", err)
	}
	defer func() { err = errors.Join(err, node.Close()) }()

	// Everything below stops before the node closes.
	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	srvs := newServers(logger)
	var appURL atomic.Pointer[string]
	if setupLn != nil {
		setupURL := localURL(setupLn.Addr())
		srvs.start(api.Setup(api.SetupOptions{
			Tailnet: node,
			AppURL: func() string {
				if u := appURL.Load(); u != nil {
					return *u
				}
				return ""
			},
			Logger: logger,
		}), setupLn)
		logger.Info("serving the setup page", "url", setupURL)
		if !loopback(setupLn.Addr()) {
			logger.Warn("other machines can reach the setup page, and until Roost is logged in, anyone who opens it "+
				"can add Roost to their own tailnet; listen on 127.0.0.1 (in a container, publish the port on "+
				"127.0.0.1 only), or log in with TS_AUTHKEY", "address", setupLn.Addr().String())
		}
		wg.Go(func() { announceLogin(ctx, node, setupURL, logger) })
	}

	type listenResult struct {
		ln  net.Listener
		url string
		err error
	}
	listened := make(chan listenResult, 1)
	wg.Go(func() {
		ln, url, err := node.Listen(ctx)
		listened <- listenResult{ln, url, err}
	})
	var res listenResult
	select {
	case res = <-listened:
	case <-srvs.failed:
		return srvs.stop(ctx)
	case <-ctx.Done():
		logger.Info("shutting down")
		return srvs.stop(ctx)
	}
	if res.err != nil {
		if ctx.Err() != nil {
			return srvs.stop(ctx)
		}
		return errors.Join(fmt.Errorf("listen on the tailnet: %w", res.err), srvs.stop(ctx))
	}

	if err := addFirstAdmins(ctx, st, node, cfg.Admins, logger); err != nil {
		return errors.Join(err, res.ln.Close(), srvs.stop(ctx))
	}
	// Everything that follows reads what admins changed in the settings from
	// here, so a change applies at once.
	prefs := settings.NewCache(st, logger)
	cpuWindow := func() time.Duration { return prefs.Get(ctx).Thresholds.CPUWindow() }
	registry := agents.NewRegistry(st, logger)
	registry.SetWindow(cpuWindow)
	if err := registry.Load(ctx); err != nil {
		// The agents report again within seconds.
		logger.Warn("read the agents' last reports", "error", err)
	}
	recorder := usage.NewRecorder(st, logger)
	wg.Go(func() { recorder.Run(ctx) })
	// Checks dial through the node, which reaches the tailnet's addresses
	// and names as well as the internet.
	runner := checks.NewRunner(st, checks.Options{Dial: node.Dial, Logger: logger})
	if err := runner.Load(ctx); err != nil {
		return errors.Join(fmt.Errorf("load the checks: %w", err), res.ln.Close(), srvs.stop(ctx))
	}
	wg.Go(func() { runner.Run(ctx) })
	engine := alerts.NewEngine(st, logger)
	if err := engine.Load(ctx); err != nil {
		return errors.Join(fmt.Errorf("load the open alerts: %w", err), res.ln.Close(), srvs.stop(ctx))
	}

	poller := tailnet.NewPoller(node, logger)
	wg.Go(func() { poller.Run(ctx) })
	// SSH reaches peers through the node, and the machine Roost runs on
	// through its loopback address.
	remote := ssh.NewManager(ssh.Options{
		KeyDir:   filepath.Join(cfg.DataDir, "ssh"),
		Store:    st,
		Tailnet:  poller.Snapshot,
		Dial:     node.Dial,
		HostMode: cfg.Tailscale == config.Host,
		Logger:   logger,
	})
	if err := remote.Load(ctx); err != nil {
		return errors.Join(fmt.Errorf("load SSH access: %w", err), res.ln.Close(), srvs.stop(ctx))
	}
	feedOpts := api.FeedOptions{
		Version:        buildVersion(),
		Tailnet:        poller.Snapshot,
		TailnetChanged: poller.Changes(),
		Agents:         registry,
		Checks:         runner,
		Alerts:         engine,
		SSH:            remote.Access,
		SSHChanged:     remote.Changes(),
		Store:          st,
		Settings:       prefs,
		Logger:         logger,
	}
	if hostmetrics.Supported {
		// Tests build a Config by hand, without the default.
		hostRoot := cmp.Or(cfg.HostRoot, config.DefaultHostRoot)
		host := hostmetrics.NewCollector(hostmetrics.Local(hostRoot), logger)
		host.SetWindow(cpuWindow)
		wg.Go(func() { host.Run(ctx) })
		feedOpts.Host, feedOpts.HostChanged = host.Reading, host.Changes()
		sampler := usage.NewSampler(os.DirFS(hostRoot))
		wg.Go(func() {
			sampler.Run(ctx, logger, func(s usage.Sample) { recorder.Record(ctx, store.HubID, s) })
		})
	}
	var localDocker *docker.Client
	if socket, ok := docker.Socket(); ok {
		localDocker = docker.NewClient(docker.SocketDialer(socket))
		containers := docker.NewCollector(localDocker, logger)
		wg.Go(func() { containers.Run(ctx) })
		feedOpts.Docker, feedOpts.DockerChanged = containers.Reading, containers.Changes()
	}
	// The Docker of the machine Roost runs on is on its socket, and the
	// others' are on theirs, over SSH.
	fleet := docker.NewFleet(localDocker, remote.DialDocker)
	defer fleet.Close()
	sampler := docker.NewSampler(docker.SamplerOptions{
		Targets: func() []docker.Target { return dockerTargets(fleet, poller.Snapshot(), remote.Access()) },
		Record:  st.AddContainerMetrics,
		Logger:  logger,
	})
	wg.Go(func() { sampler.Run(ctx) })
	var units *systemd.Collector
	if systemd.Available() {
		units = systemd.NewCollector(systemd.Options{
			UserScope: systemd.HasUserScope(),
			Watched:   func() []string { return prefs.Get(ctx).WatchedUnits },
			Logger:    logger,
		})
		wg.Go(func() { units.Run(ctx) })
		feedOpts.Systemd, feedOpts.SystemdChanged = units.Reading, units.Changes()
	}
	feed := api.NewFeed(feedOpts)
	// Notifications go out through the node too, so a channel can send to a
	// server on the tailnet as well as to one on the internet.
	notifier := notify.NewDispatcher(notify.Options{
		Store:    st,
		Open:     engine.Open,
		Settings: prefs,
		AppURL:   res.url,
		Env:      notify.NewEnv(node.Dial),
		Changed:  feed.Poke,
		Logger:   logger,
	})
	// The app checks requests against Roost's own names, from the first status.
	select {
	case <-poller.Polled():
	case <-ctx.Done():
		logger.Info("shutting down")
		return errors.Join(res.ln.Close(), srvs.stop(ctx))
	}
	wg.Go(func() { feed.Run(ctx) })
	wg.Go(func() { notifier.Run(ctx) })
	wg.Go(func() { remote.Run(ctx) })
	stopping := make(chan struct{})
	app := srvs.start(api.New(api.Options{
		Version:  buildVersion(),
		UI:       ui,
		DevUI:    devUI,
		WhoIs:    node.WhoIs,
		Snapshot: poller.Snapshot,
		Feed:     feed,
		Store:    st,
		Agents:   registry,
		Usage:    recorder,
		Checks:   runner,
		Alerts:   engine,
		SSH:      remote,
		Docker:   fleet,
		Settings: prefs,
		Notify:   notifier,
		Systemd:  units,
		// config.Parse has checked it.
		Tailscale: api.TailscaleMode(cfg.Tailscale),
		AppURL:    res.url,
		Stopping:  stopping,
		Logger:    logger,
	}), res.ln)
	// Shutdown waits for every response to finish, and live streams don't
	// until they're told to.
	app.RegisterOnShutdown(func() { close(stopping) })
	appURL.Store(&res.url)
	logger.Info("roost is running", "version", buildVersion(), "url", res.url, "data_dir", cfg.DataDir)

	select {
	case <-srvs.failed:
	case <-ctx.Done():
		logger.Info("shutting down")
	}
	return srvs.stop(ctx)
}

// dockerTargets returns the machines whose containers Roost records the use
// of: the one it runs on, if it has Docker, and the online ones whose SSH
// access found a Docker it can use.
func dockerTargets(fleet *docker.Fleet, snap tailnet.Snapshot, access map[string]ssh.Access) []docker.Target {
	var out []docker.Target
	if h, err := fleet.Host(store.HubID); err == nil {
		out = append(out, docker.Target{ID: store.HubID, Host: h})
	}
	if snap.Status == nil {
		return out
	}
	for _, d := range snap.Status.Peers {
		a := access[d.ID]
		// In embedded mode, the machine Roost runs on is a peer too, which
		// was added already.
		if d.SameMachine || !d.Online || a.State != ssh.StateOK || a.Docker == nil || !*a.Docker {
			continue
		}
		if h, err := fleet.Host(d.ID); err == nil {
			out = append(out, docker.Target{ID: d.ID, Host: h})
		}
	}
	return out
}

// addFirstAdmins makes the owner of Roost's node and the logins in admins
// Roost's admins, if it has none yet. A tagged node has no owner.
func addFirstAdmins(ctx context.Context, st *store.Store, tn tailnet.Tailnet, admins []string, logger *slog.Logger) error {
	status, err := tn.Status(ctx)
	if err != nil {
		return fmt.Errorf("get the tailnet status: %w", err)
	}
	logins := admins
	if owner := status.Self.Owner; owner != "" {
		logins = append([]string{owner}, admins...)
	}
	added, err := st.AddFirstAdmins(ctx, logins)
	if err != nil {
		return err
	}
	if added {
		logger.Info("made Roost's first admins", "logins", strings.Join(logins, ","))
		return nil
	}
	users, err := st.Users(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(users, func(u store.User) bool { return u.Role == store.Admin }) {
		logger.Warn("Roost has no admins, since its node is tagged and ROOST_ADMINS is empty; " +
			"add one with `roost admin add <login>`")
	}
	return nil
}

// announceLogin logs the login URL each time Tailscale makes a new one, until
// the node is on the tailnet.
func announceLogin(ctx context.Context, tn tailnet.Tailnet, setupURL string, logger *slog.Logger) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var last string
	for {
		if st, err := tn.Status(ctx); err == nil {
			if st.State == tailnet.StateRunning {
				return
			}
			if st.LoginURL != "" && st.LoginURL != last {
				last = st.LoginURL
				logger.Info("log Roost in to your tailnet with the setup page or the login URL",
					"setup_page", setupURL, "login_url", st.LoginURL)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// localURL returns an http URL for a local listener's address, naming
// localhost if it listens on every address.
func localURL(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok || !tcp.IP.IsUnspecified() {
		return "http://" + addr.String()
	}
	return "http://" + net.JoinHostPort("localhost", strconv.Itoa(tcp.Port))
}

// loopback reports whether addr is a loopback address, which only this machine
// can reach.
func loopback(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}

func devUIURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("dev UI: %w", err)
	}
	return u, nil
}

// servers runs HTTP servers and stops them together.
type servers struct {
	logger  *slog.Logger
	running []runningServer
	// failed is signaled when a server stops by itself. stop returns its error.
	failed chan struct{}
}

type runningServer struct {
	srv  *http.Server
	done chan error
}

func newServers(logger *slog.Logger) *servers {
	return &servers{logger: logger, failed: make(chan struct{}, 1)}
}

// start serves h on ln, and returns the server.
func (s *servers) start(h http.Handler, ln net.Listener) *http.Server {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelWarn),
	}
	done := make(chan error, 1)
	s.running = append(s.running, runningServer{srv: srv, done: done})
	go func() {
		err := srv.Serve(ln)
		done <- err
		if !errors.Is(err, http.ErrServerClosed) {
			select {
			case s.failed <- struct{}{}:
			default:
			}
		}
	}()
	return srv
}

// stop shuts every server down, giving in-flight requests shutdownTimeout to
// finish, and returns any error a server stopped with.
func (s *servers) stop(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	var errs []error
	for _, r := range s.running {
		if err := r.srv.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shut down: %w", err))
		}
	}
	for _, r := range s.running {
		if err := <-r.done; !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("serve: %w", err))
		}
	}
	return errors.Join(errs...)
}
