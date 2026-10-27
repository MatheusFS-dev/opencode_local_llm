package maestro

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

// Status is a recorded job lifecycle state, independent of model narration.
type Status string

const (
	Queued           Status = "queued"
	Running          Status = "running"
	Verifying        Status = "verifying"
	AwaitingReview   Status = "awaiting_review"
	ChangesRequested Status = "changes_requested"
	Completed        Status = "completed"
	Failed           Status = "failed"
	Cancelled        Status = "cancelled"
	TimedOut         Status = "timed_out"
)

// Job contains the exact task, model, settings, outcome, and process identity.
type Job struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	Request      worker.Request `json:"request"`
	Settings     config.Config  `json:"settings"`
	Status       Status         `json:"status"`
	Created      time.Time      `json:"created"`
	Updated      time.Time      `json:"updated"`
	Deadline     time.Time      `json:"deadline"`
	PID          int            `json:"pid"`
	ProcessStamp string         `json:"process_stamp,omitempty"`
	Error        string         `json:"error,omitempty"`
	Review       string         `json:"review,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
	Attempt      int            `json:"attempt"`
	UserTask     string         `json:"user_task"`
	ChildID      string         `json:"child_id,omitempty"`
}

// Store keeps private, atomic job records and unmodified external output.
type Store struct{ Root string }

// LockController prevents duplicate review decisions or correction workers during reconnects.
func (store Store) LockController(id string) (io.Closer, error) {
	directory, err := store.Directory(id)
	if err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "controller.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("job %s already has an active controller; reconnect after it exits", id)
	}
	return lock, nil
}

func validID(id string) bool {
	data, err := hex.DecodeString(id)
	return err == nil && len(data) == 16
}

// Directory returns a validated job directory.
func (store Store) Directory(id string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("invalid Maestro job ID")
	}
	return filepath.Join(store.Root, id), nil
}

// Create records a queued job before starting its independent process.
func (store Store) Create(request worker.Request, settings config.Config, provider string, timeout time.Duration) (Job, error) {
	if _, err := worker.BuildArguments(request); err != nil {
		return Job{}, err
	}
	if !filepath.IsAbs(request.Project) {
		return Job{}, fmt.Errorf("project directory must be absolute")
	}
	if timeout <= 0 || timeout > 24*time.Hour {
		return Job{}, fmt.Errorf("job timeout must be greater than zero and at most 24 hours")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Job{}, err
	}
	now := time.Now().UTC()
	job := Job{ID: hex.EncodeToString(id[:]), Request: request, Settings: settings, Provider: provider, Status: Queued, Created: now, Updated: now, Deadline: now.Add(timeout), Attempt: 1}
	return job, store.Save(job)
}

// Save atomically replaces a private job record.
func (store Store) Save(job Job) error {
	directory, err := store.Directory(job.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	job.Updated = time.Now().UTC()
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(directory, ".job-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), filepath.Join(directory, "job.json"))
}

// Load reads a durable job state. Quiet output does not affect its status.
func (store Store) Load(id string) (Job, error) {
	directory, err := store.Directory(id)
	if err != nil {
		return Job{}, err
	}
	data, err := os.ReadFile(filepath.Join(directory, "job.json"))
	if err != nil {
		return Job{}, fmt.Errorf("read Maestro job %s: %w", id, err)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, err
	}
	if job.ID != id {
		return Job{}, fmt.Errorf("Maestro job identity mismatch")
	}
	return job, nil
}

// Start launches a detached runner, so a lost controller connection cannot kill its worker.
func (store Store) Start(binary string, job Job) error {
	directory, err := store.Directory(job.ID)
	if err != nil {
		return err
	}
	launch, err := os.OpenFile(filepath.Join(directory, "launch.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer launch.Close()
	if err := syscall.Flock(int(launch.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	job, err = store.Load(job.ID)
	if err != nil {
		return err
	}
	if job.Status != Queued {
		return fmt.Errorf("job %s is already %s", job.ID, job.Status)
	}
	if store.CancellationRequested(job.ID) {
		job.Status, job.Error = Cancelled, "job cancelled before worker start"
		return store.Save(job)
	}
	if job.PID > 0 {
		return nil
	}
	log, err := os.OpenFile(filepath.Join(directory, "runner.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	command := exec.Command(binary, "maestro-job", "--store", store.Root, "--id", job.ID)
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		job.Status, job.Error = Failed, err.Error()
		if saveErr := store.Save(job); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	job.PID = command.Process.Pid
	job.ProcessStamp, _ = processStamp(job.PID)
	if err := store.Save(job); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	go command.Wait()
	return nil
}

// Execute owns the worker deadline and records verification separately from review.
func (store Store) Execute(ctx context.Context, id string, run func(context.Context, Job, io.Writer, func() error) error) error {
	directory, err := store.Directory(id)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(directory, "runner.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("job already has a runner: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	launch, err := os.OpenFile(filepath.Join(directory, "launch.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer launch.Close()
	if err := syscall.Flock(int(launch.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	job, err := store.Load(id)
	if err != nil {
		return err
	}
	if job.Status != Queued {
		return fmt.Errorf("cannot execute job in state %s", job.Status)
	}
	if store.CancellationRequested(id) {
		job.Status, job.Error = Cancelled, "job cancelled before worker start"
		return store.Save(job)
	}
	job.Status, job.PID = Running, os.Getpid()
	job.ProcessStamp, _ = processStamp(job.PID)
	if err := store.Save(job); err != nil {
		return err
	}
	_ = syscall.Flock(int(launch.Fd()), syscall.LOCK_UN)
	log, err := os.OpenFile(filepath.Join(directory, "worker.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		job.Status, job.Error = Failed, err.Error()
		return errors.Join(err, store.Save(job))
	}
	defer log.Close()
	deadline, cancel := context.WithDeadline(ctx, job.Deadline)
	defer cancel()
	verified := false
	err = run(deadline, job, log, func() error {
		verified = true
		job.Status = Verifying
		return store.Save(job)
	})
	switch {
	case errors.Is(deadline.Err(), context.DeadlineExceeded):
		job.Status, job.Error = TimedOut, "configured job deadline exceeded"
		err = errors.Join(deadline.Err(), err)
	case errors.Is(deadline.Err(), context.Canceled):
		job.Status, job.Error = Cancelled, "job cancelled"
		err = errors.Join(deadline.Err(), err)
	case err != nil:
		job.Status, job.Error = Failed, err.Error()
	case !verified:
		job.Status, job.Error = Failed, "worker exited without entering independent verification"
		err = errors.New(job.Error)
	default:
		job.Status = AwaitingReview
	}
	if err != nil && !strings.Contains(job.Error, err.Error()) {
		job.Error += ": " + err.Error()
	}
	return errors.Join(err, store.Save(job))
}

// Wait polls durable state until work ends, while optionally reporting honest progress.
func (store Store) Wait(ctx context.Context, id string, progress func(Job)) (Job, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastReport := time.Time{}
	for {
		job, err := store.Load(id)
		if err != nil {
			return Job{}, err
		}
		if job.Status != Queued && job.Status != Running && job.Status != Verifying {
			return job, nil
		}
		if progress != nil && time.Since(lastReport) >= 10*time.Second {
			progress(job)
			lastReport = time.Now()
		}
		// A process identity mismatch, observed exit, or configured deadline is evidence, not silence.
		stamp, identityErr := processStamp(job.PID)
		dead := job.PID > 0 && (identityErr != nil || (job.ProcessStamp != "" && stamp != job.ProcessStamp))
		if dead || (job.PID == 0 && time.Now().After(job.Deadline)) {
			// The runner may have committed its final record between our first read and observed exit.
			current, err := store.Load(id)
			if err != nil {
				return Job{}, err
			}
			if current.Status != Queued && current.Status != Running && current.Status != Verifying {
				return current, nil
			}
			if current.PID != job.PID || current.ProcessStamp != job.ProcessStamp {
				continue
			}
			job = current
			job.Status, job.Error = Failed, "Maestro runner exited without a final result; inspect runner.log and worker.log"
			if store.CancellationRequested(job.ID) {
				job.Status, job.Error = Cancelled, "job cancelled"
			} else if job.PID == 0 {
				job.Status, job.Error = TimedOut, "configured deadline elapsed before the worker started"
			}
			if err := store.Save(job); err != nil {
				return Job{}, err
			}
			return job, nil
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-ticker.C:
		}
	}
}

// Review records a controller review only after independent verification succeeded.
func (store Store) Review(id string, approved bool, reason string) error {
	lock, err := store.lockTransition(id)
	if err != nil {
		return err
	}
	defer lock.Close()
	job, err := store.Load(id)
	if err != nil {
		return err
	}
	if job.Status != AwaitingReview || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("review requires an awaiting_review job and a reason")
	}
	if store.CancellationRequested(id) {
		job.Status, job.Error = Cancelled, "job cancelled before review completed"
		if err := store.Save(job); err != nil {
			return err
		}
		return errors.New(job.Error)
	}
	job.Status, job.Review = ChangesRequested, reason
	if approved {
		job.Status = Completed
	}
	return store.Save(job)
}

// Cancel signals only the independently owned runner identified by its job record.
func (store Store) Cancel(id string) error {
	lock, err := store.lockTransition(id)
	if err != nil {
		return err
	}
	defer lock.Close()
	job, err := store.Load(id)
	if err != nil {
		return err
	}
	if job.Status != Queued && job.Status != Running && job.Status != Verifying && job.Status != AwaitingReview {
		return fmt.Errorf("job has no cancellable work")
	}
	directory, _ := store.Directory(id)
	if err := os.WriteFile(filepath.Join(directory, "cancel.requested"), []byte("cancel\n"), 0o600); err != nil {
		return err
	}
	if job.PID == 0 || job.Status == AwaitingReview {
		return nil
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", job.PID))
	if err != nil || !strings.Contains(string(data), "maestro-job\x00") || !strings.Contains(string(data), "\x00"+id+"\x00") {
		return fmt.Errorf("runner identity cannot be verified; refusing to signal PID %d", job.PID)
	}
	return syscall.Kill(job.PID, syscall.SIGTERM)
}

func (store Store) lockTransition(id string) (*os.File, error) {
	directory, err := store.Directory(id)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "transition.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// CancellationRequested is a separate marker so cancellation cannot overwrite a runner's state update.
func (store Store) CancellationRequested(id string) bool {
	directory, err := store.Directory(id)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(directory, "cancel.requested"))
	return err == nil
}

func processStamp(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process has not started")
	}
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	end := strings.LastIndex(string(data), ")")
	if end < 0 {
		return "", fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(data)[end+1:])
	if len(fields) < 20 || fields[0] == "Z" {
		return "", fmt.Errorf("process has exited")
	}
	return fields[19], nil
}

// Correction deterministically links one child job, including recovery after a controller crash.
func (store Store) Correction(parent Job, timeout time.Duration) (Job, error) {
	if parent.Status != ChangesRequested || parent.Attempt >= 3 {
		return Job{}, fmt.Errorf("job cannot start another correction")
	}
	digest := sha256.Sum256([]byte(parent.ID + "/correction"))
	id := hex.EncodeToString(digest[:16])
	child, err := store.Load(id)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Job{}, err
	}
	if err != nil {
		now := time.Now().UTC()
		request := parent.Request
		request.Task += "\n\n" + parent.Review
		child = Job{ID: id, Provider: parent.Provider, Request: request, Settings: parent.Settings, Status: Queued, Created: now, Updated: now, Deadline: now.Add(timeout), ParentID: parent.ID, Attempt: parent.Attempt + 1, UserTask: parent.UserTask}
		if err := store.Save(child); err != nil {
			return Job{}, err
		}
	} else if child.ParentID != parent.ID {
		return Job{}, fmt.Errorf("correction job identity mismatch")
	}
	parent.ChildID = child.ID
	if err := store.Save(parent); err != nil {
		return Job{}, err
	}
	return child, nil
}
