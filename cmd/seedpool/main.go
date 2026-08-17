package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"seedpool"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("seedpool", flag.ContinueOnError)
	flags.SetOutput(stderr)
	address := flags.String("addr", "", "HTTP listen address (default :8080 in serve mode)")
	serve := flags.Bool("serve", false, "serve HTTP requests until stopped")
	smoke := flags.Bool("smoke", false, "run the bounded HTTP workflow and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *serve && *smoke {
		return fmt.Errorf("-serve and -smoke cannot be used together")
	}

	if *smoke {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := seedpool.RunSmoke(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "seedpool smoke: ok")
		return nil
	}

	if *serve {
		if *address == "" {
			*address = ":8080"
		}
		server := &http.Server{Addr: *address, Handler: seedpool.NewServer(seedpool.NewStore(nil))}
		fmt.Fprintf(stderr, "seedpool listening on %s\n", *address)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}

	if *address == "" {
		*address = "127.0.0.1:0"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := checkStartup(ctx, *address); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "seedpool startup: ok")
	return nil
}

func checkStartup(ctx context.Context, address string) (returnErr error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: seedpool.NewServer(seedpool.NewStore(nil))}
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.Serve(listener)
	}()
	defer func() {
		closeErr := server.Close()
		serveErr := <-serveErrors
		if returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close startup server: %w", closeErr)
		}
		if returnErr == nil && serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			returnErr = fmt.Errorf("serve startup request: %w", serveErr)
		}
	}()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return fmt.Errorf("resolve startup address: %w", err)
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	endpoint := (&url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/rounds"}).String()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create startup request: %w", err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send startup request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		return fmt.Errorf("startup request returned HTTP %d", response.StatusCode)
	}
	return nil
}
