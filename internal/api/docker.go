package api

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/manasseh-fikadu/roost/internal/docker"
	"github.com/manasseh-fikadu/roost/internal/ssh"
	"github.com/manasseh-fikadu/roost/internal/store"
)

const (
	// logBatch is how many lines of a container's logs go in one event at
	// most, and logFlush how long lines wait to be sent. The first lines of a
	// long tail come in big batches, and the lines that follow in quick small
	// ones.
	logBatch = 1000
	logFlush = 80 * time.Millisecond
	// defaultTail is how many lines a log starts with while it follows.
	defaultTail = 500
	// maxTail is the most lines a log can start with.
	maxTail = 100_000
	// logReadSize is how much of a container's logs Roost reads at once.
	logReadSize = 32 << 10
	// diskWait and volumeWait are how long a page waits for the daemon to
	// measure its disk use before it shows what it has.
	diskWait   = 2500 * time.Millisecond
	mountWait  = 1500 * time.Millisecond
	volumeWait = 8 * time.Second
)

// dockerTarget is the Docker a request is about.
type dockerTarget struct {
	host *docker.Host
	// deviceID is the ID in the path, and historyID the ID its history is
	// kept under.
	deviceID, historyID string
}

// dockerOf finds the Docker of the device in the path. If it can't, it
// answers with why, and returns nil.
func (h *handler) dockerOf(w http.ResponseWriter, r *http.Request) *dockerTarget {
	d, hid, ok := h.device(r)
	if !ok {
		h.notFound(w, r)
		return nil
	}
	name := h.deviceName(r, d.ID)
	if hid != store.HubID {
		a, ok := h.opts.SSH.Access()[hid]
		var why string
		switch {
		case !ok:
			why = "SSH access isn't set up for " + name + ", so Roost can't reach its Docker"
		case a.DockerSocket == "":
			why = "Roost found no Docker socket on " + name + "; if it has Docker, check the machine again on its page"
		}
		if why != "" {
			if callerOf(r).level < admins {
				why = cantReachDevice
			}
			h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: why})
			return nil
		}
	}
	host, err := h.opts.Docker.Host(hid)
	if errors.Is(err, docker.ErrNoDocker) {
		why := "Roost finds no Docker socket on the machine it runs on"
		if callerOf(r).level < admins {
			why = cantReachDevice
		}
		h.writeJSON(w, r, http.StatusConflict, ErrorBody{Error: why})
		return nil
	}
	if err != nil {
		h.fail(w, r, "find a machine's Docker", err)
		return nil
	}
	return &dockerTarget{host: host, deviceID: d.ID, historyID: hid}
}

// dockerContainerOf finds the Docker of the device in the path and the
// container in it. If it can't, it answers with why, once, and returns false.
func (h *handler) dockerContainerOf(w http.ResponseWriter, r *http.Request) (*dockerTarget, string, bool) {
	t := h.dockerOf(w, r)
	if t == nil {
		return nil, "", false
	}
	cid, ok := h.containerID(w, r)
	return t, cid, ok
}

// containerID returns the container in the path. If it can't be one, it
// answers with an error and returns false.
func (h *handler) containerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("cid")
	if !docker.ValidID(id) {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: docker.ErrBadID.Error()})
		return "", false
	}
	return id, true
}

// Said to someone who may not know how Roost reaches a machine: its SSH user
// and port, its Docker socket, or what Docker itself says went wrong.
const (
	cantReach       = "Roost can't reach Docker on this machine right now"
	cantReachDevice = "Roost can't reach this machine's Docker"
)

// dockerStatus returns the HTTP status and message for an error from Docker,
// or from reaching it. An error about how Roost reaches the machine says
// where, and as what user, so only admins get its words: Device.SSH is for
// admins too. Everyone gets what Docker says of a request that was wrong, such
// as a container that isn't there.
func dockerStatus(err error, admin bool) (int, string) {
	var (
		de *docker.Error
		se *ssh.SocketError
	)
	reach := func(status int, msg string) (int, string) {
		if admin {
			return status, msg
		}
		return status, cantReach
	}
	switch {
	case errors.As(err, &de):
		status := de.ClientStatus()
		msg := cmp.Or(de.Message, de.Error())
		if status == http.StatusBadGateway {
			// The daemon failed, and its message can name paths.
			return reach(status, msg)
		}
		return status, msg
	case errors.Is(err, docker.ErrBadID):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, docker.ErrNoDocker), errors.Is(err, ssh.ErrNoDockerSocket):
		return reach(http.StatusConflict, err.Error())
	case errors.Is(err, docker.ErrUnreachable):
		return reach(http.StatusBadGateway, err.Error())
	case errors.As(err, &se):
		return reach(http.StatusBadGateway, "Docker isn't reachable: "+se.Error()+"; is Docker running, and may the SSH user use it?")
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "Docker didn't answer in time"
	}
	if status, known := sshStatus(err); known {
		return reach(status, err.Error())
	}
	return reach(http.StatusBadGateway, "Docker: "+err.Error())
}

// dockerError answers with the status and message for an error from Docker,
// and reports whether there was one.
func (h *handler) dockerError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if r.Context().Err() != nil {
		// The caller went away.
		return true
	}
	status, msg := dockerStatus(err, callerOf(r).level >= admins)
	h.writeJSON(w, r, status, ErrorBody{Error: msg})
	return true
}

// together runs the functions at once and returns the first error.
func together(fns ...func() error) error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Go(func() { errs[i] = fn() })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// dockerOverview answers GET /api/devices/{id}/docker/overview.
func (h *handler) dockerOverview(w http.ResponseWriter, r *http.Request) {
	t := h.dockerOf(w, r)
	if t == nil {
		return
	}
	var (
		info docker.Info
		disk *docker.DiskUsage
	)
	err := together(
		func() (err error) { info, err = t.host.Info(r.Context()); return },
		func() error { disk = t.host.Disk(r.Context(), diskWait); return nil },
	)
	if h.dockerError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, dockerOverview(info, disk))
}

// dockerContainers answers GET /api/devices/{id}/docker/containers.
func (h *handler) dockerContainers(w http.ResponseWriter, r *http.Request) {
	t := h.dockerOf(w, r)
	if t == nil {
		return
	}
	entries, err := t.host.Entries(r.Context())
	if h.dockerError(w, r, err) {
		return
	}
	out := DockerContainerList{FetchedAt: time.Now(), Containers: make([]DockerContainerRow, len(entries))}
	for i, e := range entries {
		out.Containers[i] = dockerRow(e)
	}
	slices.SortFunc(out.Containers, func(a, b DockerContainerRow) int { return strings.Compare(a.Name, b.Name) })
	h.writeJSON(w, r, http.StatusOK, out)
}

// dockerContainer answers GET /api/devices/{id}/docker/containers/{cid}.
func (h *handler) dockerContainer(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	in, err := t.host.Client().Inspect(ctx, cid)
	if h.dockerError(w, r, err) {
		return
	}
	var (
		image *docker.RawImageInfo
		disk  *docker.DiskUsage
	)
	_ = together(
		// A container whose image is gone has no image to say more of.
		func() error { image, _ = t.host.Client().ImageInfo(ctx, in.Image); return nil },
		func() error {
			if in.HasVolume() {
				disk = t.host.Disk(ctx, mountWait)
			}
			return nil
		},
	)
	h.writeJSON(w, r, http.StatusOK, dockerDetail(in, image, disk))
}

// dockerInspect answers GET /api/devices/{id}/docker/containers/{cid}/inspect
// with the container's whole inspection, which has its environment and
// commands in it, so it is for admins.
func (h *handler) dockerInspect(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	raw, err := t.host.Client().InspectRaw(r.Context(), cid)
	if h.dockerError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, raw)
}

// dockerTop answers GET /api/devices/{id}/docker/containers/{cid}/top with
// the container's processes.
func (h *handler) dockerTop(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	top, err := t.host.Client().Top(r.Context(), cid)
	if h.dockerError(w, r, err) {
		return
	}
	out := DockerProcesses{Titles: nonNil(top.Titles), Processes: top.Processes}
	if out.Processes == nil {
		out.Processes = [][]string{}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// dockerHistory answers GET
// /api/devices/{id}/docker/containers/{cid}/history.
func (h *handler) dockerHistory(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	from, to, step, ok := h.historyRange(w, r)
	if !ok {
		return
	}
	// The history outlives the container, so a name that Docker no longer
	// knows is the name the history was kept under.
	name, err := t.host.Name(r.Context(), cid)
	if err != nil {
		name = cid
	}
	buckets, err := h.opts.Store.ContainerHistory(r.Context(), t.historyID, name, from, step)
	if err != nil {
		h.fail(w, r, "read a container's history", err)
		return
	}
	out := DockerHistory{
		From: from, To: to, Step: step.Milliseconds(),
		CPU: make([]Point, len(buckets)), Mem: make([]Point, len(buckets)),
		NetRx: make([]Point, len(buckets)), NetTx: make([]Point, len(buckets)),
		BlkRead: make([]Point, len(buckets)), BlkWrite: make([]Point, len(buckets)),
	}
	for i, b := range buckets {
		ts := b.Start.UnixMilli()
		out.CPU[i], out.Mem[i] = Point{ts, b.CPU}, Point{ts, b.Mem}
		out.NetRx[i], out.NetTx[i] = Point{ts, b.NetRx}, Point{ts, b.NetTx}
		out.BlkRead[i], out.BlkWrite[i] = Point{ts, b.BlkRead}, Point{ts, b.BlkWrite}
	}
	h.writeJSON(w, r, http.StatusOK, out)
}

// dockerVolumes answers GET /api/devices/{id}/docker/volumes.
func (h *handler) dockerVolumes(w http.ResponseWriter, r *http.Request) {
	t := h.dockerOf(w, r)
	if t == nil {
		return
	}
	ctx := r.Context()
	var (
		raw  []docker.RawVolume
		list []docker.RawContainer
		disk *docker.DiskUsage
	)
	err := together(
		func() (err error) { raw, err = t.host.Client().Volumes(ctx); return },
		func() (err error) { list, err = t.host.Containers(ctx); return },
		func() error { disk = t.host.Disk(ctx, volumeWait); return nil },
	)
	if h.dockerError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, dockerVolumes(raw, list, disk))
}

// dockerImages answers GET /api/devices/{id}/docker/images.
func (h *handler) dockerImages(w http.ResponseWriter, r *http.Request) {
	t := h.dockerOf(w, r)
	if t == nil {
		return
	}
	ctx := r.Context()
	var (
		raw  []docker.RawImage
		list []docker.RawContainer
	)
	err := together(
		func() (err error) { raw, err = t.host.Client().Images(ctx); return },
		func() (err error) { list, err = t.host.Containers(ctx); return },
	)
	if h.dockerError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, dockerImages(raw, list))
}

// dockerNetworks answers GET /api/devices/{id}/docker/networks.
func (h *handler) dockerNetworks(w http.ResponseWriter, r *http.Request) {
	t := h.dockerOf(w, r)
	if t == nil {
		return
	}
	ctx := r.Context()
	var (
		raw  []docker.RawNetwork
		list []docker.RawContainer
	)
	err := together(
		func() (err error) { raw, err = t.host.Client().Networks(ctx); return },
		func() (err error) { list, err = t.host.Containers(ctx); return },
	)
	if h.dockerError(w, r, err) {
		return
	}
	h.writeJSON(w, r, http.StatusOK, dockerNetworks(raw, list))
}

// dockerAction answers POST /api/devices/{id}/docker/containers/{cid}/start,
// stop and restart, which an admin can do, and which the activity log keeps.
func (h *handler) dockerAction(a docker.Action, done string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t, cid, ok := h.dockerContainerOf(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		// The name is what the log says, and the container may be gone once
		// it has stopped, if it removes itself.
		name, err := t.host.Name(ctx, cid)
		if h.dockerError(w, r, err) {
			return
		}
		device := h.deviceName(r, t.deviceID)
		changed, err := t.host.Do(ctx, cid, a)
		if err == nil && changed {
			// Docker acted, whether or not the admin is still there to hear.
			h.addEvent(context.WithoutCancel(ctx), store.Event{
				Kind: string(EventKindContainer), DeviceID: t.deviceID, Actor: callerOf(r).who.LoginName,
				Message: done + " " + name + " on " + device,
			})
		}
		if h.dockerError(w, r, err) {
			return
		}
		h.writeJSON(w, r, http.StatusOK, DockerActionResult{Changed: changed})
	}
}

// logOptions reads which logs to read from the query of r. A log that
// follows starts with its latest lines, and one that doesn't has all of
// them.
func logOptions(r *http.Request, follow bool) (docker.LogOptions, error) {
	q := r.URL.Query()
	o := docker.LogOptions{Follow: follow, Stdout: q.Get("stdout") != "0", Stderr: q.Get("stderr") != "0", Tail: -1}
	if follow {
		o.Tail = defaultTail
	}
	if tail := q.Get("tail"); tail == "all" {
		o.Tail = -1
	} else if tail != "" {
		n, err := strconv.Atoi(tail)
		if err != nil || n < 0 {
			return o, errors.New("the tail must be a number of lines, or all")
		}
		o.Tail = min(n, maxTail)
	}
	var err error
	o.Since, err = docker.ParseSince(q.Get("since"), time.Now())
	return o, err
}

// dockerLogs answers GET /api/devices/{id}/docker/containers/{cid}/logs,
// which is a stream of server-sent events: a "start", then "lines" in
// batches, and then an "end", or a "failure" if it broke. Logs often print
// secrets, so it's for admins.
func (h *handler) dockerLogs(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	opts, err := logOptions(r, r.URL.Query().Get("follow") != "0")
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	in, err := t.host.Client().Inspect(r.Context(), cid)
	if h.dockerError(w, r, err) {
		return
	}
	ctx, cancel := h.streamContext(r)
	defer cancel()
	c := callerOf(r)
	s := openSSE(w, r)
	defer s.close()
	if s.send("start", DockerLogStart{TTY: in.Config.Tty, Running: in.State.Running}) != nil {
		return
	}
	if !opts.Stdout && !opts.Stderr {
		_ = s.send("end", DockerStreamEnd{Reason: "Select stdout or stderr"})
		return
	}
	body, dec, err := t.host.Client().Logs(ctx, in.ID, in.Config.Tty, opts)
	if err != nil {
		if ctx.Err() == nil {
			_, msg := dockerStatus(err, true)
			_ = s.send("failure", DockerStreamFailure{Message: msg})
		}
		return
	}
	defer func() { _ = body.Close() }()

	// Reading happens apart from writing, so lines can wait for the batch
	// to fill or the timer to run out while Docker is quiet.
	type chunk struct {
		lines []docker.LogLine
		err   error
	}
	chunks := make(chan chunk)
	go func() {
		defer close(chunks)
		buf := make([]byte, logReadSize)
		for {
			n, err := body.Read(buf)
			var lines []docker.LogLine
			if n > 0 {
				lines = dec.Push(buf[:n], nil)
			}
			if err != nil {
				lines = dec.Flush(lines)
			}
			if len(lines) > 0 || err != nil {
				select {
				case chunks <- chunk{lines, err}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	var (
		batch  []DockerLogLine
		timer  = time.NewTimer(time.Hour)
		timerC <-chan time.Time
	)
	timer.Stop()
	defer timer.Stop()
	flush := func() error {
		timer.Stop()
		timerC = nil
		if len(batch) == 0 {
			return nil
		}
		err := s.send("lines", batch)
		batch = nil
		return err
	}
	keepAlive, roles := time.NewTicker(sseKeepAlive), time.NewTicker(roleCheckInterval)
	defer keepAlive.Stop()
	defer roles.Stop()
	for {
		select {
		case ch, open := <-chunks:
			if !open {
				return
			}
			for _, l := range ch.lines {
				batch = append(batch, DockerLogLine{S: l.Stream, T: l.Time, M: l.Text})
			}
			switch {
			case len(batch) >= logBatch:
				if flush() != nil {
					return
				}
			case timerC == nil && len(batch) > 0:
				timer.Reset(logFlush)
				timerC = timer.C
			}
			if ch.err == nil {
				continue
			}
			if flush() != nil {
				return
			}
			switch {
			case ctx.Err() != nil:
			case !errors.Is(ch.err, io.EOF):
				_ = s.send("failure", DockerStreamFailure{Message: ch.err.Error()})
			case !opts.Follow:
				_ = s.send("end", DockerStreamEnd{Reason: "End of log"})
			case in.State.Running:
				_ = s.send("end", DockerStreamEnd{Reason: "The container stopped"})
			default:
				_ = s.send("end", DockerStreamEnd{Reason: "The container isn't running"})
			}
			return
		case <-timerC:
			if flush() != nil {
				return
			}
		case <-keepAlive.C:
			if s.keepAlive() != nil {
				return
			}
		case <-roles.C:
			if h.callerRoleChanged(ctx, c) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// fileSafe matches what a file name made from a container's name can't have.
var fileSafe = regexp.MustCompile(`[^\w.-]`)

// dockerLogDownload answers GET
// /api/devices/{id}/docker/containers/{cid}/logs/download with the log as a
// plain text file, without terminal escapes.
func (h *handler) dockerLogDownload(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	opts, err := logOptions(r, false)
	if err != nil {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: err.Error()})
		return
	}
	if !opts.Stdout && !opts.Stderr {
		h.writeJSON(w, r, http.StatusBadRequest, ErrorBody{Error: "select stdout or stderr"})
		return
	}
	withTimes := r.URL.Query().Get("timestamps") != "0"
	// A download can be long, so it ends when the server shuts down too.
	ctx, cancel := h.streamContext(r)
	defer cancel()
	in, err := t.host.Client().Inspect(ctx, cid)
	if h.dockerError(w, r, err) {
		return
	}
	body, dec, err := t.host.Client().Logs(ctx, in.ID, in.Config.Tty, opts)
	if h.dockerError(w, r, err) {
		return
	}
	defer func() { _ = body.Close() }()

	file := fileSafe.ReplaceAllString(strings.TrimPrefix(in.Name, "/"), "_") + "-" + time.Now().UTC().Format("2006-01-02-15-04-05") + ".log"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+file+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Each write has a deadline, so a client that stops reading ends the
	// download instead of holding Docker's stream and the connection open.
	out := io.Writer(&deadlineWriter{w: w, rc: http.NewResponseController(w)})
	if acceptsGzip(r) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gz := gzip.NewWriter(out)
		defer func() { _ = gz.Close() }()
		out = gz
	}
	w.WriteHeader(http.StatusOK)
	var text bytes.Buffer
	write := func(lines []docker.LogLine) error {
		text.Reset()
		for _, l := range lines {
			if withTimes && l.Time != "" {
				text.WriteString(l.Time + " ")
			}
			text.WriteString(docker.StripANSI(l.Text))
			text.WriteByte('\n')
		}
		_, err := out.Write(text.Bytes())
		return err
	}
	buf := make([]byte, logReadSize)
	for {
		n, err := body.Read(buf)
		if n > 0 && write(dec.Push(buf[:n], nil)) != nil {
			// The client went away, or stopped reading.
			return
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			_ = write(dec.Flush(nil))
			return
		case ctx.Err() != nil:
			// The client went away, or the server is stopping.
			return
		default:
			// Docker or the connection to it broke. A file that ends here
			// would look as whole as one that was, so the response is cut off
			// instead, and the browser says the download failed.
			h.log.Warn("read a container's log for a download", "error", err)
			panic(http.ErrAbortHandler)
		}
	}
}

// A deadlineWriter gives each write to a response a deadline, if the
// connection supports one.
type deadlineWriter struct {
	w  io.Writer
	rc *http.ResponseController
}

func (d *deadlineWriter) Write(p []byte) (int, error) {
	if err := setWriteDeadline(d.rc, time.Now().Add(streamWriteTimeout)); err != nil {
		return 0, err
	}
	n, err := d.w.Write(p)
	if err != nil {
		return n, err
	}
	return n, setWriteDeadline(d.rc, time.Time{})
}

// dockerStats answers GET
// /api/devices/{id}/docker/containers/{cid}/stats, which is a stream of
// server-sent events: a "backlog" of the latest few minutes, then a "sample"
// each second, and an "end" once the container stops. Everyone watching a
// container shares one stream from Docker.
func (h *handler) dockerStats(w http.ResponseWriter, r *http.Request) {
	t, cid, ok := h.dockerContainerOf(w, r)
	if !ok {
		return
	}
	in, err := t.host.Client().Inspect(r.Context(), cid)
	if h.dockerError(w, r, err) {
		return
	}
	ctx, cancel := h.streamContext(r)
	defer cancel()
	c := callerOf(r)
	s := openSSE(w, r)
	defer s.close()
	var watch *docker.Watch
	if in.State.Running {
		watch = t.host.Watch(in.ID)
	}
	if watch == nil {
		if s.send("backlog", []DockerStats{}) == nil {
			_ = s.send("end", DockerStreamEnd{Reason: "The container isn't running"})
		}
		return
	}
	defer watch.Close()
	backlog := make([]DockerStats, len(watch.Recent))
	for i, st := range watch.Recent {
		backlog[i] = *dockerStats(st)
	}
	if s.send("backlog", backlog) != nil {
		return
	}
	keepAlive, roles := time.NewTicker(sseKeepAlive), time.NewTicker(roleCheckInterval)
	defer keepAlive.Stop()
	defer roles.Stop()
	for {
		select {
		case st := <-watch.C:
			if s.send("sample", dockerStats(st)) != nil {
				return
			}
		case <-watch.Done():
			_ = s.send("end", DockerStreamEnd{Reason: watch.Reason()})
			return
		case <-keepAlive.C:
			if s.keepAlive() != nil {
				return
			}
		case <-roles.C:
			if h.callerRoleChanged(ctx, c) {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
