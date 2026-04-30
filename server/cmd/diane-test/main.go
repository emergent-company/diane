// diane-test is a Discord-based integration test harness for Diane.
//
// It connects to Discord as a separate bot, sends test messages to a
// configured channel, and verifies Diane's responses, reactions, and
// thread creation behavior.
//
// Usage:
//
//	# Set environment variables:
//	export TEST_BOT_TOKEN="<test-harness-bot-token>"
//	export TEST_CHANNEL_ID="<discord-channel-id>"
//	export DIANE_BOT_ID="<diane-bot-user-id>"
//
//	# Run all tests:
//	diane-test
//
//	# Run specific tests:
//	diane-test -test basic-ping -test thread-continuation
//
// Setup:
//  1. Create a new Discord bot for the test harness in Discord Dev Portal
//  2. Enable Message Content, Server Members, and Reaction intents
//  3. Invite the test bot to the server
//  4. Add the test bot's user ID to Diane's config:
//     discord_test_bot_ids: ["<test-bot-user-id>"]
//  5. Set the environment variables above
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Emergent-Comapny/diane/internal/testharness"
)

func main() {
	var (
		botToken    = flag.String("token", os.Getenv("TEST_BOT_TOKEN"), "Test harness bot token (or TEST_BOT_TOKEN env)")
		channelID   = flag.String("channel", os.Getenv("TEST_CHANNEL_ID"), "Test channel ID (or TEST_CHANNEL_ID env)")
		targetBotID = flag.String("target-bot", os.Getenv("DIANE_BOT_ID"), "Diane's Discord user ID (or DIANE_BOT_ID env)")
		testFilter  = flag.String("test", "", "Comma-separated test names to run (empty = all)")
		verbose     = flag.Bool("v", false, "Verbose logging")
	)
	flag.Parse()

	if *botToken == "" {
		log.Fatal("TEST_BOT_TOKEN not set and --token not provided")
	}
	if *channelID == "" {
		log.Fatal("TEST_CHANNEL_ID not set and --channel not provided")
	}
	if *targetBotID == "" {
		log.Fatal("DIANE_BOT_ID not set and --target-bot not provided")
	}

	// Parse test filter
	var filter map[string]bool
	if *testFilter != "" {
		filter = make(map[string]bool)
		for _, name := range strings.Split(*testFilter, ",") {
			filter[strings.TrimSpace(name)] = true
		}
	}

	// Configure logging
	logFlags := log.Ltime | log.Lmsgprefix
	if *verbose {
		logFlags |= log.Lshortfile
	}
	log.SetFlags(logFlags)
	log.SetPrefix("[test] ")

	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  🧪 Diane Integration Test Harness")
	fmt.Println("═══════════════════════════════════════")
	fmt.Printf("  Test channel: %s\n", *channelID)
	fmt.Printf("  Target bot:   %s\n", *targetBotID)
	fmt.Println("───────────────────────────────────────")

	// Create harness
	h, err := testharness.New(testharness.Config{
		BotToken:    *botToken,
		ChannelID:   *channelID,
		TargetBotID: *targetBotID,
	})
	if err != nil {
		log.Fatalf("Failed to create harness: %v", err)
	}
	defer h.Close()

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\n⚠️  Interrupted — shutting down...")
		h.Close()
		os.Exit(1)
	}()

	// Get test suite
	suite := testharness.DefaultTestSuite()

	// TODO: Also support YAML-driven test suite loading
	// yamlSuite := loadYAMLTestSuite("tests.yaml")
	// for name, fn := range yamlSuite { suite[name] = fn }

	// Run tests
	var results []testharness.Result
	total := 0
	passed := 0

	for name, testFn := range suite {
		if filter != nil && !filter[name] {
			continue
		}
		total++

		// Clean up any leftover threads from previous runs
		h.CleanupChannel()

		result := testFn(h)
		results = append(results, result)

		if result.Passed {
			passed++
		}

		// Small delay between tests to let Discord settle
		time.Sleep(2 * time.Second)
	}

	// Print summary
	fmt.Println("")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("  📊 Test Summary")
	fmt.Println("═══════════════════════════════════════")
	failed := total - passed
	for _, r := range results {
		status := "✅"
		if !r.Passed {
			status = "❌"
		}
		errInfo := ""
		if r.Error != "" {
			errInfo = " — " + r.Error
		}
		fmt.Printf("  %s %s (%v)%s\n", status, r.Name, r.Duration.Round(time.Millisecond), errInfo)
	}
	fmt.Println("───────────────────────────────────────")
	fmt.Printf("  %d/%d passed", passed, total)
	if failed > 0 {
		fmt.Printf(", %d failed ⚠️", failed)
	}
	fmt.Println("")
	fmt.Println("═══════════════════════════════════════")

	if failed > 0 {
		os.Exit(1)
	}
}
