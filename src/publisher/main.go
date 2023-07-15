// This example declares a durable exchange, and publishes one messages to that
// exchange. This example allows up to 8 outstanding publisher confirmations
// before blocking publishing.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	uri          = flag.String("uri", "amqp://guest:guest@localhost:5672/", "AMQP URI")
	exchange     = flag.String("exchange", "test-exchange", "Durable AMQP exchange name")
	exchangeType = flag.String("exchange-type", "direct", "Exchange type - direct|fanout|topic|x-custom")
	queue        = flag.String("queue", "test-queue", "Ephemeral AMQP queue name")
	routingKey   = flag.String("key", "test-key", "AMQP routing key")
    publishRate  = flag.Duration("publish-rate", 1*time.Second, "the publish rate")
	continuous   = flag.Bool("continuous", false, "Keep publishing messages at a 1msg/sec rate")
	WarnLog      = log.New(os.Stderr, "[WARNING] ", log.LstdFlags|log.Lmsgprefix)
	ErrLog       = log.New(os.Stderr, "[ERROR] ", log.LstdFlags|log.Lmsgprefix)
	Log          = log.New(os.Stdout, "[INFO] ", log.LstdFlags|log.Lmsgprefix)
)

func init() {
	flag.Parse()
}

func main() {
	exitCh := make(chan struct{})
	confirmsCh := make(chan *amqp.DeferredConfirmation)
	confirmsDoneCh := make(chan struct{})
	// Note: this is a buffered channel so that indicating OK to
	// publish does not block the confirm handler
	publishOkCh := make(chan struct{}, 1)

	setupCloseHandler(exitCh)

	publish(context.Background(), publishOkCh, confirmsCh, confirmsDoneCh, exitCh, *publishRate)
}

func setupCloseHandler(exitCh chan struct{}) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		Log.Printf("close handler: Ctrl+C pressed in Terminal")
		close(exitCh)
	}()
}

type Message struct {
	Id int `json:"id"`
}

func publish(ctx context.Context, publishOkCh <-chan struct{}, confirmsCh chan<- *amqp.DeferredConfirmation, confirmsDoneCh <-chan struct{}, exitCh chan struct{}, publishRate time.Duration) {
	config := amqp.Config{
		Vhost:      "/",
		Properties: amqp.NewConnectionProperties(),
	}
	config.Properties.SetClientConnectionName("producer-with-confirms")

	Log.Printf("producer: dialing %s", *uri)
	conn, err := amqp.DialConfig(*uri, config)
	if err != nil {
		ErrLog.Fatalf("producer: error in dial: %s", err)
	}
	defer conn.Close()

	Log.Println("producer: got Connection, getting Channel")
	channel, err := conn.Channel()
	if err != nil {
		ErrLog.Fatalf("error getting a channel: %s", err)
	}
	defer channel.Close()

	Log.Printf("producer: declaring exchange")
	if err := channel.ExchangeDeclare(
		*exchange,     // name
		*exchangeType, // type
		true,          // durable
		false,         // auto-delete
		false,         // internal
		false,         // noWait
		nil,           // arguments
	); err != nil {
		ErrLog.Fatalf("producer: Exchange Declare: %s", err)
	}

	Log.Printf("producer: declaring queue '%s'", *queue)
	queue, err := channel.QueueDeclare(
		*queue, // name of the queue
		true,   // durable
		false,  // delete when unused
		false,  // exclusive
		false,  // noWait
		nil,    // arguments
	)
	if err == nil {
		Log.Printf("producer: declared queue (%q %d messages, %d consumers), binding to Exchange (key %q)",
			queue.Name, queue.Messages, queue.Consumers, *routingKey)
	} else {
		ErrLog.Fatalf("producer: Queue Declare: %s", err)
	}

	Log.Printf("producer: declaring binding")
	if err := channel.QueueBind(queue.Name, *routingKey, *exchange, false, nil); err != nil {
		ErrLog.Fatalf("producer: Queue Bind: %s", err)
	}

	// Reliable publisher confirms require confirm.select support from the
	// connection.
	Log.Printf("producer: enabling publisher confirms.")
	if err := channel.Confirm(false); err != nil {
		ErrLog.Fatalf("producer: channel could not be put into confirm mode: %s", err)
	}
	Id := 0
	for {
		body, err := json.Marshal(Message{Id: Id})
		if err != nil {
			ErrLog.Fatalf("could not marshal data")
		}
		Log.Printf("producer: publishing %dB body (%q)", len(body), body)

		err = channel.PublishWithContext(
			ctx,
			*exchange,
			*routingKey,
			true,
			false,
			amqp.Publishing{
				Headers:         amqp.Table{},
				ContentType:     "text/plain",
				ContentEncoding: "",
				DeliveryMode:    amqp.Persistent,
				Priority:        0,
				AppId:           "sequential-producer",
				Body:            body,
			},
		)
		if err != nil {
			ErrLog.Fatalf("producer: error in publish: %s", err)
		}
		select {
		case <-exitCh:
            return
        case <-time.After(publishRate):
            Id += 1
		}
	}
}
