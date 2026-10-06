package config

import "time"

// Config holds the configuration for the proxy server
type Config struct {
	WorkersSecretNamespace string
	RequestTimeout         time.Duration
	DefaultLogTailLines    int
	ClientQPS              float32
	ClientBurst            int
	HubQPS                 float32
	HubBurst               int
}
