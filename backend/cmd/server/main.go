package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"image-web/backend/internal/app"
	"image-web/backend/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()
	application, err := app.New(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()

	go func() {
		fmt.Println("server listening on :" + cfg.Port)
		if err := application.Server.ListenAndServe(); err != nil && err.Error() != "http: Server closed" {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = application.Server.Shutdown(shutdownCtx)
}
