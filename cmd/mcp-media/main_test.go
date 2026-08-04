package main

import (
	"context"
	"strings"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

type blockingVideoProvider struct {
	result *MediaResult
	wait   chan struct{}
}

func (f *blockingVideoProvider) GenerateVideo(ctx context.Context, prompt, model, imagePath string) (*MediaResult, error) {
	if f.wait != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.wait:
		}
	}
	return f.result, nil
}

func (f *blockingVideoProvider) VideoModels() []ModelInfo {
	return []ModelInfo{{ID: "fake-video", Provider: "fake", Type: "video"}}
}

func TestGenerateVideoHandlerReturnsJobIDForRunningManagedJob(t *testing.T) {
	wait := make(chan struct{})
	provider := &blockingVideoProvider{
		result: &MediaResult{Path: "/tmp/generated.mp4", MimeType: "video/mp4"},
		wait:   wait,
	}
	reg := NewRegistry()
	reg.RegisterVideo(provider)
	jobs := NewMediaJobStore(time.Hour, 0)
	server := newMediaServer(reg, jobs, time.Minute, time.Minute, 10*time.Millisecond)

	client, err := mcpclient.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer client.Close()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start client: %v", err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "mcp-media-test", Version: "1"}
	if _, err := client.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize client: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "generate_video"
	req.Params.Arguments = map[string]any{"prompt": "make a launch clip"}
	result, err := client.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("call generate_video: %v", err)
	}
	text := toolResultText(t, result)
	if result.IsError {
		t.Fatalf("generate_video returned error: %s", text)
	}
	if !strings.Contains(text, "Job ID: media_") || !strings.Contains(text, "Do not start another generation") {
		t.Fatalf("generate_video did not return managed-job polling guidance:\n%s", text)
	}

	fields := strings.Fields(text)
	var jobID string
	for i, field := range fields {
		if field == "ID:" && i+1 < len(fields) {
			jobID = fields[i+1]
			break
		}
	}
	if jobID == "" {
		t.Fatalf("could not extract job id from response:\n%s", text)
	}

	close(wait)
	job := waitForMediaJob(t, jobs, jobID, mediaJobSucceeded)
	if job.Path != "/tmp/generated.mp4" || job.MimeType != "video/mp4" {
		t.Fatalf("completed job = %+v", job)
	}
}

func TestGenerateVideoRejectsRelativeImagePath(t *testing.T) {
	provider := &blockingVideoProvider{
		result: &MediaResult{Path: "/tmp/generated.mp4", MimeType: "video/mp4"},
	}
	reg := NewRegistry()
	reg.RegisterVideo(provider)
	jobs := NewMediaJobStore(time.Hour, 0)
	server := newMediaServer(reg, jobs, time.Minute, time.Minute, 10*time.Millisecond)

	client, err := mcpclient.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer client.Close()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start client: %v", err)
	}
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "mcp-media-test", Version: "1"}
	if _, err := client.Initialize(context.Background(), initReq); err != nil {
		t.Fatalf("initialize client: %v", err)
	}

	req := mcp.CallToolRequest{}
	req.Params.Name = "generate_video"
	req.Params.Arguments = map[string]any{"prompt": "make a launch clip", "image_path": "relative.png"}
	result, err := client.CallTool(context.Background(), req)
	if err != nil {
		t.Fatalf("call generate_video: %v", err)
	}
	text := toolResultText(t, result)
	if !result.IsError || !strings.Contains(text, "absolute path on the mcp-media host filesystem") {
		t.Fatalf("relative image_path result IsError=%v text=%q", result.IsError, text)
	}
}

func toolResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, content := range result.Content {
		text, ok := content.(mcp.TextContent)
		if !ok {
			t.Fatalf("unsupported content type: %T", content)
		}
		b.WriteString(text.Text)
	}
	return b.String()
}
