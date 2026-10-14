// Command dlq-replay republishes JSON-lines records exported from a *.dlq topic.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/example/live-platform/internal/mq"
)

func main() {
	var brokers, inputPath, actor string
	flag.StringVar(&brokers, "brokers", env("KAFKA_BROKERS", "localhost:19092"), "comma-separated Kafka brokers")
	flag.StringVar(&inputPath, "input", "-", "DLQ JSON-lines file, or - for stdin")
	flag.StringVar(&actor, "actor", "", "operator identity recorded in the replay audit (required)")
	flag.Parse()
	if strings.TrimSpace(actor) == "" {
		fmt.Fprintln(os.Stderr, "-actor is required")
		os.Exit(2)
	}

	input, closeInput, err := openInput(inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer closeInput()

	producer, err := mq.NewProducer(strings.Split(brokers, ","), slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "create Kafka producer:", err)
		os.Exit(1)
	}
	defer producer.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	count, err := mq.ReplayJSONLines(ctx, input, producer, actor, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "replayed %d record(s)\n", count)
}

func openInput(path string) (io.Reader, func(), error) {
	if path == "-" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, fmt.Errorf("open input: %w", err)
	}
	return f, func() { _ = f.Close() }, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
