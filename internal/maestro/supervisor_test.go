package maestro

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/MatheusFS-dev/opencode_local_llm/internal/config"
	"github.com/MatheusFS-dev/opencode_local_llm/internal/worker"
)

type fixedController struct {
	approve bool
	project string
}

func (controller fixedController) Plan(context.Context, string) (Plan, error) {
	return Plan{Task: "Create result.sh", Criteria: "Executable file", Verify: "test -x result.sh", Artifacts: []worker.Artifact{{Path: filepath.Join(controller.project, "result.sh"), Executable: true}}}, nil
}
func (controller fixedController) Review(context.Context, Job, string) (Review, error) {
	value := controller.approve
	return Review{Approved: &value, Reason: "Inspected result", Correction: "Fix requested behavior"}, nil
}

func TestSupervisorDoesNotFinishBeforeReview(t *testing.T) {
	store := Store{Root: t.TempDir()}
	project := t.TempDir()
	supervisor := Supervisor{Store: store, Project: project, Provider: "codex", Settings: config.Config{SelectedModel: "local:4b", ContextLength: 16384}, Controller: fixedController{approve: true, project: project}, Timeout: time.Minute}
	supervisor.Start = func(job Job) error {
		return store.Execute(t.Context(), job.ID, func(_ context.Context, _ Job, _ io.Writer, verifying func() error) error { return verifying() })
	}
	job, err := supervisor.Run(t.Context(), "Create file", "")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != Completed || job.Review == "" {
		t.Fatalf("job=%#v", job)
	}
}

func TestSupervisorStopsOnActualWorkerFailure(t *testing.T) {
	store := Store{Root: t.TempDir()}
	project := t.TempDir()
	supervisor := Supervisor{Store: store, Project: project, Provider: "codex", Settings: config.Config{SelectedModel: "local:4b", ContextLength: 16384}, Controller: fixedController{approve: true, project: project}, Timeout: time.Minute}
	supervisor.Start = func(job Job) error {
		// A background runner records its own failure, independently of start success.
		_ = store.Execute(t.Context(), job.ID, func(context.Context, Job, io.Writer, func() error) error { return errors.New("model unavailable") })
		return nil
	}
	job, err := supervisor.Run(t.Context(), "Create file", "")
	if err == nil || job.Status != Failed || job.Review != "" {
		t.Fatalf("job=%#v err=%v", job, err)
	}
}

func TestSupervisorLimitsReviewCorrectionRounds(t *testing.T) {
	store := Store{Root: t.TempDir()}
	project := t.TempDir()
	supervisor := Supervisor{Store: store, Project: project, Provider: "codex", Settings: config.Config{SelectedModel: "local:4b", ContextLength: 16384}, Controller: fixedController{approve: false, project: project}, Timeout: time.Minute}
	supervisor.Start = func(job Job) error {
		return store.Execute(t.Context(), job.ID, func(_ context.Context, _ Job, _ io.Writer, verifying func() error) error { return verifying() })
	}
	job, err := supervisor.Run(t.Context(), "Create file", "")
	if err == nil || job.Attempt != 3 || job.Status != ChangesRequested {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	resumed, err := supervisor.Run(t.Context(), "", job.ID)
	if err == nil || resumed.Attempt != 3 {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
}
