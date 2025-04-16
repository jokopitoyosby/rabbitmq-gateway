package consumer

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jokopitoyosby/rabbitmq-gateway/models"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitMQConsumer struct {
	config    models.Config
	exchange  string
	queues    []string
	callbacks map[string]func([]byte) error

	conn    *amqp.Connection
	channel *amqp.Channel
	mu      sync.Mutex

	done    chan struct{}
	wg      sync.WaitGroup
	isReady bool
	notify  chan struct{}
}

func NewConsumer(configFile, exchange string, queues []string, callbacks map[string]func([]byte) error) *RabbitMQConsumer {
	config, err := parseConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	return &RabbitMQConsumer{
		config:    config,
		exchange:  exchange,
		queues:    queues,
		callbacks: callbacks,
		done:      make(chan struct{}),
		notify:    make(chan struct{}, 1),
	}
}

func (c *RabbitMQConsumer) Start() {
	c.wg.Add(1)
	go c.handleReconnect()

	// Tunggu koneksi pertama berhasil
	<-c.notify
}

func (c *RabbitMQConsumer) handleReconnect() {
	defer c.wg.Done()

	for {
		c.mu.Lock()
		c.isReady = false
		c.mu.Unlock()

		err := c.connect()
		if err != nil {
			log.Printf("Failed to connect. Retrying... Error: %v", err)
			select {
			case <-c.done:
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		c.mu.Lock()
		c.isReady = true
		c.mu.Unlock()

		// Notify bahwa koneksi sudah ready
		select {
		case c.notify <- struct{}{}:
		default:
		}

		// Monitor koneksi
		closeChan := c.conn.NotifyClose(make(chan *amqp.Error))
		select {
		case err := <-closeChan:
			log.Printf("Connection closed: %v", err)
		case <-c.done:
			return
		}
	}
}

func (c *RabbitMQConsumer) connect() error {
	conn, err := amqp.DialConfig(
		fmt.Sprintf("amqp://%s:%s@%s:%d/",
			c.config.Username,
			c.config.Password,
			c.config.Host,
			c.config.Port),
		amqp.Config{
			Heartbeat: 10 * time.Second,
			Locale:    "en_US",
		},
	)
	if err != nil {
		return err
	}

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}

	// err = ch.ExchangeDeclare(
	// 	c.exchange,
	// 	"direct",
	// 	true,
	// 	false,
	// 	false,
	// 	false,
	// 	nil,
	// )
	// if err != nil {
	// 	conn.Close()
	// 	return err
	// }

	for _, queue := range c.queues {
		_, err = ch.QueueDeclare(
			queue,
			true,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			conn.Close()
			return err
		}

		// err = ch.QueueBind(
		// 	queue,
		// 	queue,
		// 	c.exchange,
		// 	false,
		// 	nil,
		// )
		// if err != nil {
		// 	conn.Close()
		// 	return err
		// }

		msgs, err := ch.Consume(
			queue,
			"",
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			conn.Close()
			return err
		}

		c.wg.Add(1)
		go c.handleMessages(queue, msgs)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn = conn
	c.channel = ch

	return nil
}

func (c *RabbitMQConsumer) handleMessages(queue string, msgs <-chan amqp.Delivery) {
	defer c.wg.Done()

	for msg := range msgs {
		callback, ok := c.callbacks[queue]
		if !ok {
			log.Printf("No callback for queue %s", queue)
			continue
		}

		if err := callback(msg.Body); err != nil {
			log.Printf("Callback error: %v", err)
			continue
		}

		if err := msg.Ack(false); err != nil {
			log.Printf("Failed to ack message: %v", err)
		}
	}

	log.Printf("Message channel closed for queue %s", queue)
}

func (c *RabbitMQConsumer) Stop() {
	close(c.done)
	c.wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}

// parseConfig membaca dan mem-parsing file konfigurasi JSON
func parseConfig(configFile string) (models.Config, error) {
	var config models.Config

	file, err := os.ReadFile(configFile)
	if err != nil {
		return config, fmt.Errorf("error reading config file: %v", err)
	}

	err = json.Unmarshal(file, &config)
	if err != nil {
		return config, fmt.Errorf("error parsing config JSON: %v", err)
	}

	if config.Host == "" {
		return config, errors.New("host is required in config")
	}
	if config.Port == 0 {
		config.Port = 5672 // default port
	}

	return config, nil
}
