package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeImageProvider struct {
	result    *MediaResult
	err       error
	wait      chan struct{}
	ignoreCtx bool
}

func (f *fakeImageProvider) GenerateImage(ctx context.Context, prompt, model, size, aspectRatio string) (*MediaResult, error) {
	if f.wait != nil {
		if f.ignoreCtx {
			<-f.wait
		} else {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.wait:
			}
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeImageProvider) EditImage(ctx context.Context, imagePath, prompt, model string) (*MediaResult, error) {
	return f.GenerateImage(ctx, prompt, model, "", "")
}

func (f *fakeImageProvider) ImageModels() []ModelInfo {
	return []ModelInfo{{ID: "fake-image", Provider: "fake", Type: "image"}}
}

func TestMediaJobStoreStartImageReturnsImmediatelyAndCompletes(t *testing.T) {
	wait := make(chan struct{})
	store := NewMediaJobStore(time.Hour, 0)
	provider := &fakeImageProvider{
		result: &MediaResult{Path: "/tmp/generated.png", MimeType: "image/png"},
		wait:   wait,
	}

	job, err := store.Start("image", "draw a cat", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return provider.GenerateImage(ctx, "draw a cat", "fake-image", "1024x1024", "")
	})
	if err != nil {
		t.Fatalf("start image: %v", err)
	}
	if job.ID == "" || job.Status != mediaJobQueued {
		t.Fatalf("initial job = %+v, want queued with id", job)
	}

	close(wait)
	got := waitForMediaJob(t, store, job.ID, mediaJobSucceeded)
	if got.Path != "/tmp/generated.png" || got.MimeType != "image/png" || got.Model != "fake-image" {
		t.Fatalf("completed job = %+v", got)
	}
	if text := formatMediaJob(got); !strings.Contains(text, "Path: /tmp/generated.png") {
		t.Fatalf("formatted job missing path:\n%s", text)
	}
}

func TestMediaJobStoreRecordsFailure(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 0)
	provider := &fakeImageProvider{err: errors.New("provider failed")}

	job, err := store.Start("image", "draw a cat", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return provider.GenerateImage(ctx, "draw a cat", "fake-image", "", "")
	})
	if err != nil {
		t.Fatalf("start image: %v", err)
	}
	got := waitForMediaJob(t, store, job.ID, mediaJobFailed)
	if !strings.Contains(got.Error, "provider failed") {
		t.Fatalf("job error = %q", got.Error)
	}
}

func TestMediaJobStoreCancelRunningJob(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 0)
	provider := &fakeImageProvider{wait: make(chan struct{})}

	job, err := store.Start("image", "draw a cat", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return provider.GenerateImage(ctx, "draw a cat", "fake-image", "", "")
	})
	if err != nil {
		t.Fatalf("start image: %v", err)
	}
	waitForMediaJob(t, store, job.ID, mediaJobRunning)

	canceled, ok := store.Cancel(job.ID)
	if !ok {
		t.Fatal("cancel did not find job")
	}
	if canceled.Status != mediaJobCanceled {
		t.Fatalf("cancel status = %q", canceled.Status)
	}
	got := waitForMediaJob(t, store, job.ID, mediaJobCanceled)
	if got.Status != mediaJobCanceled {
		t.Fatalf("job status = %q", got.Status)
	}
}

func TestMediaJobStoreCancelIsNotOverwrittenByLateProviderResult(t *testing.T) {
	wait := make(chan struct{})
	store := NewMediaJobStore(time.Hour, 0)
	provider := &fakeImageProvider{
		result:    &MediaResult{Path: "/tmp/late.png", MimeType: "image/png"},
		wait:      wait,
		ignoreCtx: true,
	}

	job, err := store.Start("image", "draw a cat", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return provider.GenerateImage(ctx, "draw a cat", "fake-image", "", "")
	})
	if err != nil {
		t.Fatalf("start image: %v", err)
	}
	waitForMediaJob(t, store, job.ID, mediaJobRunning)
	if _, ok := store.Cancel(job.ID); !ok {
		t.Fatal("cancel did not find job")
	}
	close(wait)
	got := waitForMediaJob(t, store, job.ID, mediaJobCanceled)
	if got.Path != "" {
		t.Fatalf("canceled job should not accept late provider result: %+v", got)
	}
}

func TestMediaJobStoreSupportsGenericMediaKinds(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 0)
	job, err := store.Start("video", "make a launch clip", "fake-video", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return &MediaResult{Path: "/tmp/generated.mp4", MimeType: "video/mp4"}, nil
	})
	if err != nil {
		t.Fatalf("start generic media job: %v", err)
	}
	got := waitForMediaJob(t, store, job.ID, mediaJobSucceeded)
	if got.Kind != "video" || got.Path != "/tmp/generated.mp4" || got.MimeType != "video/mp4" {
		t.Fatalf("generic media job = %+v", got)
	}
	if text := formatMediaJobStarted(job); !strings.Contains(text, "video job started") || !strings.Contains(text, "get_media_job") {
		t.Fatalf("started response missing polling guidance:\n%s", text)
	}
}

func TestMediaJobStoreStartAndWaitReturnsCompletedJob(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 0)
	job, err := store.StartAndWait("image", "draw a cat", "fake-image", time.Minute, time.Second, func(ctx context.Context) (*MediaResult, error) {
		return &MediaResult{Path: "/tmp/generated.png", MimeType: "image/png"}, nil
	})
	if err != nil {
		t.Fatalf("start and wait: %v", err)
	}
	if job.Status != mediaJobSucceeded {
		t.Fatalf("job status = %q, want succeeded", job.Status)
	}
	text := formatAdaptiveMediaJob(job, "Image saved")
	if !strings.Contains(text, "Image saved: /tmp/generated.png") || strings.Contains(text, "Job ID:") {
		t.Fatalf("adaptive completed response = %q", text)
	}
}

func TestMediaJobStoreStartAndWaitReturnsRunningJobWithoutRetry(t *testing.T) {
	wait := make(chan struct{})
	store := NewMediaJobStore(time.Hour, 0)
	job, err := store.StartAndWait("image", "draw a cat", "fake-image", time.Minute, 10*time.Millisecond, func(ctx context.Context) (*MediaResult, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
			return &MediaResult{Path: "/tmp/generated.png", MimeType: "image/png"}, nil
		}
	})
	if err != nil {
		t.Fatalf("start and wait: %v", err)
	}
	if job.Status != mediaJobRunning && job.Status != mediaJobQueued {
		t.Fatalf("job status = %q, want queued or running", job.Status)
	}
	text := formatAdaptiveMediaJob(job, "Image saved")
	if !strings.Contains(text, "Job ID: "+job.ID) || !strings.Contains(text, "Do not start another generation") {
		t.Fatalf("adaptive running response missing polling guidance:\n%s", text)
	}

	close(wait)
	got := waitForMediaJob(t, store, job.ID, mediaJobSucceeded)
	if got.Path != "/tmp/generated.png" {
		t.Fatalf("completed path = %q", got.Path)
	}
}

func TestAdaptiveMediaJobFailureFormatsAsProviderError(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 0)
	job, err := store.StartAndWait("image", "draw a cat", "fake-image", time.Minute, time.Second, func(ctx context.Context) (*MediaResult, error) {
		return nil, errors.New("provider failed")
	})
	if err != nil {
		t.Fatalf("start and wait: %v", err)
	}
	if job.Status != mediaJobFailed {
		t.Fatalf("job status = %q, want failed", job.Status)
	}
	text := formatAdaptiveMediaJob(job, "Image saved")
	if !strings.Contains(text, "[fake-image] provider failed") {
		t.Fatalf("adaptive failure response = %q", text)
	}
}

func TestMediaJobStoreEnforcesMaxActiveJobs(t *testing.T) {
	store := NewMediaJobStore(time.Hour, 1)
	wait := make(chan struct{})
	defer close(wait)

	first, err := store.Start("image", "draw a cat", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		<-wait
		return &MediaResult{Path: "/tmp/first.png", MimeType: "image/png"}, nil
	})
	if err != nil {
		t.Fatalf("start first job: %v", err)
	}
	waitForMediaJob(t, store, first.ID, mediaJobRunning)

	if _, err := store.Start("image", "draw a dog", "fake-image", time.Minute, func(ctx context.Context) (*MediaResult, error) {
		return &MediaResult{Path: "/tmp/second.png", MimeType: "image/png"}, nil
	}); err == nil || !strings.Contains(err.Error(), "too many active media jobs") {
		t.Fatalf("second start error = %v, want active limit", err)
	}
}

func waitForMediaJob(t *testing.T, store *MediaJobStore, id, status string) *MediaJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := store.Get(id)
		if ok && job.Status == status {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := store.Get(id)
	t.Fatalf("job %s did not reach %s, got %+v", id, status, job)
	return nil
}
