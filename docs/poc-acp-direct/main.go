package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	kiroCLI := os.Getenv("KIRO_CLI_PATH")
	if kiroCLI == "" {
		kiroCLI = "kiro-cli"
	}
	cwd := os.Getenv("TEST_CWD")
	if cwd == "" {
		cwd = "/tmp"
	}
	model := os.Getenv("TEST_MODEL")

	log.SetFlags(log.Ltime | log.Lmicroseconds)

	// === Test 1: Start agent ===
	fmt.Println("\n=== TEST 1: Start Agent ===")
	agent, err := StartAgent("test-agent", kiroCLI, cwd, model)
	if err != nil {
		log.Fatalf("FAIL: start agent: %v", err)
	}
	fmt.Printf("PASS: agent started, pid=%d, session=%s\n", agent.Pid(), agent.sessionID)

	// === Test 2: Simple ask ===
	fmt.Println("\n=== TEST 2: Simple Ask ===")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	var chunks int
	response, err := agent.Ask(ctx, "Reply with exactly: HELLO_ACP_POC", func(chunk string) {
		chunks++
	})
	cancel()
	if err != nil {
		log.Fatalf("FAIL: ask: %v", err)
	}
	fmt.Printf("PASS: got response (%d bytes, %d chunks)\n", len(response), chunks)
	fmt.Printf("  Response: %.200s\n", response)

	// === Test 3: Streaming ===
	fmt.Println("\n=== TEST 3: Streaming ===")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	streamChunks := 0
	_, err = agent.Ask(ctx2, "Count from 1 to 5, one number per line.", func(chunk string) {
		streamChunks++
	})
	cancel2()
	if err != nil {
		log.Fatalf("FAIL: streaming ask: %v", err)
	}
	fmt.Printf("PASS: streaming worked, received %d chunks\n", streamChunks)

	// === Test 4: Context cancellation ===
	fmt.Println("\n=== TEST 4: Cancel ===")
	ctx3, cancel3 := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = agent.Ask(ctx3, "Write a 10000 word essay about the history of computing.", nil)
	cancel3()
	if err != nil {
		fmt.Printf("PASS: cancel worked, got expected error: %v\n", err)
	} else {
		fmt.Println("WARN: cancel test completed before timeout (agent was fast)")
	}

	// === Test 5: Ask after cancel (session still works?) ===
	fmt.Println("\n=== TEST 5: Ask After Cancel ===")
	time.Sleep(5 * time.Second) // let agent fully settle after cancel
	ctx4, cancel4 := context.WithTimeout(context.Background(), 60*time.Second)
	response2, err := agent.Ask(ctx4, "Reply with exactly: STILL_ALIVE", func(chunk string) {})
	cancel4()
	if err != nil {
		fmt.Printf("FAIL: ask after cancel: %v\n", err)
	} else {
		fmt.Printf("PASS: session survived cancel (%d bytes)\n", len(response2))
		fmt.Printf("  Response: %.200s\n", response2)
	}

	// === Test 6: Clean stop ===
	fmt.Println("\n=== TEST 6: Stop Agent ===")
	pid := agent.Pid()
	agent.Stop()
	// Verify process is gone
	time.Sleep(500 * time.Millisecond)
	proc, err := os.FindProcess(pid)
	if err == nil {
		// On Unix, FindProcess always succeeds. Check if actually alive.
		err = proc.Signal(os.Signal(nil))
	}
	if err != nil {
		fmt.Printf("PASS: process %d is gone\n", pid)
	} else {
		fmt.Printf("WARN: process %d might still be alive\n", pid)
	}

	fmt.Println("\n=== ALL TESTS COMPLETE ===")
}
