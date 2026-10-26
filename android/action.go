// Package android is a Probe action that drives an app on an Android
// emulator or device through adb: it reads the screen with uiautomator
// and acts on it with input.
package android

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/mozership/probe-android/internal/adb"
	"github.com/mozership/probe/actionrpc"
)

// Server is the android action, as Probe calls it.
type Server struct {
	log hclog.Logger
	// ctx is done once Probe stops the step, so that adb is stopped with it.
	ctx context.Context
}

var _ actionrpc.StepAction = (*Server)(nil)

// Serve serves the action to Probe.
func Serve() {
	// Probe sends SIGTERM to a step it stops, such as for its timeout, and
	// kills it a moment later. The signal stops the adb the step runs.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		time.Sleep(time.Second)
		os.Exit(1)
	}()
	actionrpc.Serve(func(log hclog.Logger) actionrpc.Action {
		return &Server{log: log, ctx: ctx}
	})
}

// Run runs a step; Probe calls RunStep.
func (a *Server) Run(with map[string]any) (map[string]any, error) {
	ret, _, err := a.RunStep(actionrpc.Call{With: with})
	return ret, err
}

// RunStep takes the actions of a step on the device.
func (a *Server) RunStep(call actionrpc.Call) (map[string]any, map[string]any, error) {
	actionrpc.LogParams(a.log, "received android action request", call.With)
	start := time.Now()
	req, err := parseReq(call.With)
	if err != nil {
		return failed(start, &Req{}, Res{}, err)
	}
	res := Res{Results: map[string]string{}, FilePaths: map[string]string{}}
	if err := a.run(req, &res); err != nil {
		return failed(start, req, res, err)
	}
	ret, err := actionrpc.EncodeResult(Result{Req: *req, Res: res, RT: time.Since(start)})
	actionrpc.LogOutcome(a.log, "android request", ret, err)
	return ret, nil, err
}

func (a *Server) run(req *Req, res *Res) error {
	ctx := a.context()
	path, err := adb.DefaultLocator().Find()
	if err != nil {
		return err
	}
	run := adb.Command(path)
	list, err := adb.Devices(ctx, run)
	if err != nil {
		return err
	}
	info, err := adb.Resolve(list, req.Device)
	if err != nil {
		return err
	}
	res.Device = info.Serial
	return runActions(ctx, adb.Device{Serial: info.Serial, Run: run}, req, res)
}

func (a *Server) context() context.Context {
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func failed(start time.Time, req *Req, res Res, err error) (map[string]any, map[string]any, error) {
	if res.Results == nil {
		res.Results = map[string]string{}
	}
	if res.FilePaths == nil {
		res.FilePaths = map[string]string{}
	}
	res.Code = 1
	ret, encErr := actionrpc.EncodeResult(Result{Req: *req, Res: res, RT: time.Since(start), Status: 1})
	if encErr != nil {
		return map[string]any{}, nil, encErr
	}
	return ret, nil, err
}
