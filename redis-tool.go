package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/proxy"
)

func main() {
	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	// Get Redis address from user
	fmt.Print("Enter Redis address (default: 127.0.0.1:6379): ")
	redisAddr, _ := reader.ReadString('\n')
	redisAddr = strings.TrimSpace(redisAddr)
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}

	// Get proxy URL from user
	fmt.Print("Enter proxy URL (leave empty for direct connection)\nExample: socks5://user:pass@127.0.0.1:1080\n> ")
	proxyURL, _ := reader.ReadString('\n')
	proxyURL = strings.TrimSpace(proxyURL)

	// Connect to Redis
	options := &redis.Options{
		Addr:         redisAddr,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Configure proxy if provided
	if proxyURL != "" {
		dialer, err := createProxyDialer(proxyURL)
		if err != nil {
			panic(fmt.Sprintf("Failed to create proxy dialer: %v", err))
		}
		options.Dialer = dialer
		fmt.Printf("Using proxy: %s\n", proxyURL)
	}

	client := redis.NewClient(options)

	// Get all keys
	keys, err := client.Keys(ctx, "*").Result()
	if err != nil {
		panic(err)
	}
	// Print kết quả và lưu kết quả vào một biến
	fmt.Printf("Found %d keys\n", len(keys))
	for _, key := range keys {
		fmt.Println(key)
	}
	// Sau khi lấy keys thì change TTL của toàn bộ các key
	// Tạo một vòng lặp để chạy lệnh EXIPIRE trên redis với toàn bộ key mang patter
	// Để cho tối ưu code có thể sẽ check TTL trước của key rồi mới đổi lại 
	

	// Sau khi đổi được TTL của key thì dump toàn bộ database


	defer client.Close()
}

// createProxyDialer creates a dialer function that routes connections through a proxy
func createProxyDialer(proxyURL string) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %v", err)
	}

	switch u.Scheme {
	case "socks5", "socks5h":
		// SOCKS5 proxy with optional authentication support
		// Supports URLs like: socks5://proxy.com:1080 (no auth)
		//                     socks5://user:pass@proxy.com:1080 (with auth)

		// Initialize auth structure as nil (no authentication by default)
		var auth *proxy.Auth

		// Check if the proxy URL contains user credentials
		// u.User will be non-nil if URL format is: socks5://username:password@host:port
		if u.User != nil {
			// Extract password from URL (returns empty string if not provided)
			password, _ := u.User.Password()

			// Create authentication structure for SOCKS5
			// This will be passed to the SOCKS5 dialer for username/password authentication
			auth = &proxy.Auth{
				User:     u.User.Username(), // Extract username from URL
				Password: password,          // Password extracted above
			}
		}

		// Create SOCKS5 dialer with the following parameters:
		// - "tcp": network type for the proxy connection
		// - u.Host: proxy server address (host:port)
		// - auth: authentication credentials (nil if no auth required)
		// - proxy.Direct: fallback dialer for direct connections if needed
		dialer, err := proxy.SOCKS5("tcp", u.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create SOCKS5 proxy: %v", err)
		}

		// Return a dialer function that wraps the SOCKS5 dialer
		// This function will be used by Redis client to establish connections
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}, nil

	case "http", "https":
		// HTTP CONNECT proxy
		return func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Connect to proxy
			proxyConn, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
			if err != nil {
				return nil, fmt.Errorf("failed to connect to proxy: %v", err)
			}

			// Send CONNECT request
			connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", addr, addr)
			if _, err := proxyConn.Write([]byte(connectReq)); err != nil {
				proxyConn.Close()
				return nil, fmt.Errorf("failed to send CONNECT request: %v", err)
			}

			// Read response (simplified - just check for 200 OK)
			buf := make([]byte, 1024)
			n, err := proxyConn.Read(buf)
			if err != nil {
				proxyConn.Close()
				return nil, fmt.Errorf("failed to read proxy response: %v", err)
			}

			response := string(buf[:n])
			if len(response) < 12 || response[9:12] != "200" {
				proxyConn.Close()
				return nil, fmt.Errorf("proxy returned non-200 response: %s", response[:min(n, 100)])
			}

			return proxyConn, nil
		}, nil

	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s (supported: socks5, http, https)", u.Scheme)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
