// This example declares a durable Exchange, an ephemeral (auto-delete) Queue,
// binds the Queue to the Exchange with a binding key, and consumes every
// message published to that Exchange with that routing key.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
    multierror "github.com/hashicorp/go-multierror"
	amqp "github.com/rabbitmq/amqp091-go"
)

var (
	uri               = flag.String("uri", "amqp://guest:guest@localhost:5672/", "AMQP URI")
	exchange          = flag.String("exchange", "test-exchange", "Durable, non-auto-deleted AMQP exchange name")
	exchangeType      = flag.String("exchange-type", "direct", "Exchange type - direct|fanout|topic|x-custom")
	queue             = flag.String("queue", "test-queue", "Ephemeral AMQP queue name")
	bindingKey        = flag.String("key", "test-key", "AMQP binding key")
	consumerTag       = flag.String("consumer-tag", "simple-consumer", "AMQP consumer tag (should not be blank)")
	lifetime          = flag.Duration("lifetime", 5*time.Second, "lifetime of process before shutdown (0s=infinite)")
	verbose           = flag.Bool("verbose", true, "enable verbose output of message data")
	autoAck           = flag.Bool("auto_ack", false, "enable message auto-ack")
	nWorkers          = flag.Int("n-workers", 1, "the number of workers to process messages")
	ErrLog            = log.New(os.Stderr, "[ERROR] ", log.LstdFlags|log.Lmsgprefix)
	Log               = log.New(os.Stdout, "[INFO] ", log.LstdFlags|log.Lmsgprefix)
	deliveryCount int = 0
)

func init() {
	flag.Parse()
}

func main() {
	c, err := NewConsumer(*uri, *exchange, *exchangeType, *queue, *bindingKey, *consumerTag, *nWorkers)
	if err != nil {
		ErrLog.Fatalf("%s", err)
	}

	SetupCloseHandler(c)

	if *lifetime > 0 {
		Log.Printf("running for %s", *lifetime)
		time.Sleep(*lifetime)
	} else {
		Log.Printf("running until Consumer is done")
		<-c.done
	}

	Log.Printf("shutting down")

	if err := c.Shutdown(); err != nil {
		ErrLog.Fatalf("error during shutdown: %s", err)
	}
}

type Consumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	tag     string
	done    chan error
    nWorkers int
}

func SetupCloseHandler(consumer *Consumer) {
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		Log.Printf("Ctrl+C pressed in Terminal")
		if err := consumer.Shutdown(); err != nil {
			ErrLog.Fatalf("error during shutdown: %s", err)
		}
		os.Exit(0)
	}()
}

func NewConsumer(amqpURI, exchange, exchangeType, queueName, key, ctag string, nWorkers int) (*Consumer, error) {
	c := &Consumer{
		conn:    nil,
		channel: nil,
		tag:     ctag,
		done:    make(chan error),
        nWorkers: nWorkers,
	}

	var err error

	config := amqp.Config{Properties: amqp.NewConnectionProperties()}
	config.Properties.SetClientConnectionName("sample-consumer")
	Log.Printf("dialing %q", amqpURI)
	c.conn, err = amqp.DialConfig(amqpURI, config)
	if err != nil {
		return nil, fmt.Errorf("dial: %s", err)
	}

	go func() {
		Log.Printf("closing: %s", <-c.conn.NotifyClose(make(chan *amqp.Error)))
	}()

	Log.Printf("got Connection, getting Channel")
	c.channel, err = c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("channel: %s", err)
	}

	Log.Printf("got Channel, declaring Exchange (%q)", exchange)
	if err = c.channel.ExchangeDeclare(
		exchange,     // name of the exchange
		exchangeType, // type
		true,         // durable
		false,        // delete when complete
		false,        // internal
		false,        // noWait
		nil,          // arguments
	); err != nil {
		return nil, fmt.Errorf("exchange Declare: %s", err)
	}

	Log.Printf("declared Exchange, declaring Queue %q", queueName)
	queue, err := c.channel.QueueDeclare(
		queueName, // name of the queue
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // noWait
		nil,       // arguments
	)
	if err != nil {
		return nil, fmt.Errorf("queue declare: %s", err)
	}

	Log.Printf("declared Queue (%q %d messages, %d consumers), binding to Exchange (key %q)",
		queue.Name, queue.Messages, queue.Consumers, key)

	if err = c.channel.QueueBind(
		queue.Name, // name of the queue
		key,        // bindingKey
		exchange,   // sourceExchange
		false,      // noWait
		nil,        // arguments
	); err != nil {
		return nil, fmt.Errorf("queue bind: %s", err)
	}

	Log.Printf("Queue bound to Exchange, starting Consume (consumer tag %q)", c.tag)
	deliveries, err := c.channel.Consume(
		queue.Name, // name
		c.tag,      // consumerTag,
		*autoAck,   // autoAck
		false,      // exclusive
		false,      // noLocal
		false,      // noWait
		nil,        // arguments
	)
	if *autoAck {
		log.Println("Autoack is enabled")
	} else {
		log.Println("Autoack is disabled")
	}
	if err != nil {
		return nil, fmt.Errorf("queue consume: %s", err)
	}

    for i := 0; i < c.nWorkers; i++ {
        go handle(deliveries, c.done, i)
    }

	return c, nil
}

func (c *Consumer) Shutdown() error {
	// will close() the deliveries channel
	if err := c.channel.Cancel(c.tag, true); err != nil {
		return fmt.Errorf("Consumer cancel failed: %s", err)
	}

	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("AMQP connection close error: %s", err)
	}

	defer Log.Printf("AMQP shutdown OK")
    var result error

	// wait for handle() to exit
    count := c.nWorkers
    for e := range c.done {
        count -= 1
        Log.Printf("Closing worker %d out of %d", c.nWorkers - count, c.nWorkers)
        if e != nil {
            result = multierror.Append(result, e)
        }
        if count == 0 {
            break
        }
    }
	return result
}

func handle(deliveries <-chan amqp.Delivery, done chan error, workerId int) {
	cleanup := func() {
		Log.Printf("handle: deliveries channel closed")
		done <- nil
	}

	defer cleanup()

	for d := range deliveries {
		deliveryCount++
		var data map[string]interface{}
		err := json.Unmarshal([]byte(d.Body), &data)
		if err != nil {
            Log.Printf("Worker %d: Error parsing json", workerId)
		}
        Log.Printf("worker %d: processing data", workerId)
		time.Sleep(1 * time.Second)
		if *verbose {
			Log.Printf(
                "worker %d: got %dB delivery: [%v] %q",
                workerId,
				len(d.Body),
				d.DeliveryTag,
				data,
			)
		} else {
			if deliveryCount%65536 == 0 {
				Log.Printf("delivery count %d", deliveryCount)
			}
		}
		if !*autoAck {
			d.Ack(false)
		}
	}
}
