package main

// Reproduce the current consumer's fetch/failure/fetch/commit sequence against
// an isolated broker. The first message deliberately represents a failed audit.
import (
 "context"
 "fmt"
 "log/slog"
 "os"
 "time"

 appkafka "github.com/frostloom/ai-gateway/internal/pkg/kafka"
)

func main() {
 addr := os.Getenv("ISOLATED_KAFKA_ADDR")
 if addr == "" { panic("ISOLATED_KAFKA_ADDR required") }
 ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second); defer cancel()
 p := appkafka.NewProducer([]string{addr}, "offset-repro", slog.Default())
 defer p.Close()
 for _, v := range []string{"audit-fails", "audit-succeeds"} {
  if err := p.Publish(ctx, "same-key", []byte(v)); err != nil { panic(err) }
 }
 c := appkafka.NewConsumer([]string{addr}, "offset-repro-group", []string{"offset-repro"})
 first, err := c.FetchMessage(ctx); if err != nil { panic(err) }
 second, err := c.FetchMessage(ctx); if err != nil { panic(err) }
 fmt.Printf("first=%s partition=%d offset=%d (simulated audit failure; no commit)\n", first.Value, first.Partition, first.Offset)
 fmt.Printf("second=%s partition=%d offset=%d (simulated audit success)\n", second.Value, second.Partition, second.Offset)
 if first.Partition != second.Partition || first.Offset >= second.Offset { panic("unexpected ordering") }
 if err := c.CommitMessages(ctx, second); err != nil { panic(err) }
 // Close flushes the actual asynchronous CommitInterval=1s consumer.
 if err := c.Close(); err != nil { panic(err) }
 c2 := appkafka.NewConsumer([]string{addr}, "offset-repro-group", []string{"offset-repro"})
 defer c2.Close()
 wait, stop := context.WithTimeout(ctx, 8*time.Second); defer stop()
 msg, err := c2.FetchMessage(wait)
 if err == nil { fmt.Printf("after restart: %s offset=%d\n", msg.Value, msg.Offset); os.Exit(2) }
 if wait.Err() != context.DeadlineExceeded { panic(err) }
 fmt.Println("after restart: no replay before deadline; first failed event skipped")
}
