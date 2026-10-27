package maestro

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

func testJob(t *testing.T, store Store) Job {
	t.Helper()
	project := t.TempDir()
	job, err := store.Create(worker.Request{Project: project, Task: "Create result.sh", Criteria: "Executable script", Verify: "test -x result.sh", Model: "local:4b", ContextLength: 16384}, config.Config{ContextLength: 16384}, "codex", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func TestJobSuccessRequiresVerificationThenReview(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	err := store.Execute(t.Context(), job.ID, func(ctx context.Context, job Job, log io.Writer, verifying func() error) error {
		current, err := store.Load(job.ID)
		if err != nil || current.Status != Running {
			t.Fatalf("running state: %#v %v", current, err)
		}
		if err := verifying(); err != nil {
			return err
		}
		current, err = store.Load(job.ID)
		if err != nil || current.Status != Verifying {
			t.Fatalf("verification state: %#v %v", current, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Load(job.ID)
	if err != nil || current.Status != AwaitingReview {
		t.Fatalf("unreviewed job = %#v %v", current, err)
	}
	if err := store.Review(job.ID, true, "Checked deliverable and acceptance output"); err != nil {
		t.Fatal(err)
	}
	current, _ = store.Load(job.ID)
	if current.Status != Completed {
		t.Fatalf("reviewed job = %s", current.Status)
	}
}

func TestWorkerFailureIsTerminalAndPreservesError(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	err := store.Execute(t.Context(), job.ID, func(context.Context, Job, io.Writer, func() error) error {
		return errors.New("worker exit status 7: model unavailable")
	})
	if err == nil {
		t.Fatal("worker failure was accepted")
	}
	current, _ := store.Load(job.ID)
	if current.Status != Failed || !strings.Contains(current.Error, "exit status 7") {
		t.Fatalf("failed job = %#v", current)
	}
	if err := store.Review(job.ID, true, "accept"); err == nil {
		t.Fatal("failed job was approved")
	}
}

func TestQuietJobWaitsForActualCompletion(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	job.Status = Running
	job.PID = os.Getpid()
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Millisecond)
	defer cancel()
	_, err := store.Wait(ctx, job.ID, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait error = %v", err)
	}
	current, _ := store.Load(job.ID)
	if current.Status != Running {
		t.Fatalf("quiet job incorrectly changed to %s", current.Status)
	}
}

func TestJobDeadlineRecordsTimeout(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	job.Deadline = time.Now().Add(-time.Second)
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	err := store.Execute(t.Context(), job.ID, func(ctx context.Context, _ Job, _ io.Writer, _ func() error) error { <-ctx.Done(); return ctx.Err() })
	if err == nil {
		t.Fatal("deadline succeeded")
	}
	current, _ := store.Load(job.ID)
	if current.Status != TimedOut {
		t.Fatalf("job = %#v", current)
	}
}

func TestJobIDsCannotEscapeStore(t *testing.T) {
	store := Store{Root: t.TempDir()}
	if _, err := store.Load(filepath.Join("..", "outside")); err == nil {
		t.Fatal("invalid job ID accepted")
	}
}

func TestOnlyOneControllerCanReviewOrResumeAJob(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	first, err := store.LockController(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := store.LockController(job.ID); err == nil {
		second.Close()
		t.Fatal("duplicate controller acquired job")
	}
	first.Close()
	second, err := store.LockController(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
}

func TestCorrectionRecoveryReusesTheSameChild(t *testing.T) {
	store := Store{Root: t.TempDir()}
	parent := testJob(t, store)
	parent.Status, parent.Review = ChangesRequested, "Fix behavior"
	if err := store.Save(parent); err != nil {
		t.Fatal(err)
	}
	first, err := store.Correction(parent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Recover the crash window where the child exists but the parent link was not saved.
	parent.ChildID = ""
	if err := store.Save(parent); err != nil {
		t.Fatal(err)
	}
	second, err := store.Correction(parent, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Attempt != 2 {
		t.Fatalf("correction duplicated: %#v %#v", first, second)
	}
	loaded, _ := store.Load(parent.ID)
	if loaded.ChildID != first.ID {
		t.Fatalf("parent link was not recovered: %#v", loaded)
	}
}

func TestQuietQueuedJobDoesNotFailAtThirtySeconds(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	job.Created = time.Now().Add(-time.Minute)
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := store.Wait(ctx, job.ID, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("quiet startup guessed as failure: %v", err)
	}
	current, _ := store.Load(job.ID)
	if current.Status != Queued {
		t.Fatalf("quiet startup status = %s", current.Status)
	}
}

func TestCancelBeforeRunnerStartsRecordsCancellation(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	if err := store.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	err := store.Execute(t.Context(), job.ID, func(context.Context, Job, io.Writer, func() error) error {
		t.Fatal("cancelled worker executed")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := store.Load(job.ID)
	if current.Status != Cancelled {
		t.Fatalf("job = %#v", current)
	}
}

func TestAcceptedCancellationCannotBeOverwrittenByReview(t *testing.T) {
	store := Store{Root: t.TempDir()}
	job := testJob(t, store)
	job.Status = AwaitingReview
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	if err := store.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Review(job.ID, true, "Looks correct"); err == nil {
		t.Fatal("cancelled review was accepted")
	}
	current, _ := store.Load(job.ID)
	if current.Status != Cancelled {
		t.Fatalf("cancellation overwritten: %#v", current)
	}
}
