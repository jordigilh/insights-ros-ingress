package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RedHatInsights/insights-ros-ingress/internal/auth"
	"github.com/RedHatInsights/insights-ros-ingress/internal/config"
	"github.com/RedHatInsights/insights-ros-ingress/internal/health"
	"github.com/RedHatInsights/insights-ros-ingress/internal/logger"
	"github.com/RedHatInsights/insights-ros-ingress/internal/messaging"
	"github.com/RedHatInsights/insights-ros-ingress/internal/storage"
	"github.com/RedHatInsights/insights-ros-ingress/internal/upload"
	"github.com/go-chi/chi/v5"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	log := logger.InitLogger()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	log.WithFields(logrus.Fields{
		"service": "insights-ros-ingress",
		"version": "1.0.0",
		"port":    cfg.Server.Port,
	}).Info("Starting Insights ROS Ingress service")

	// Initialize storage client
	storageClient, err := storage.NewMinIOClient(cfg.Storage)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize storage client")
	}

	// Initialize messaging client
	messagingClient, err := messaging.NewKafkaProducer(cfg.Kafka)
	if err != nil {
		log.WithError(err).Fatal("Failed to initialize messaging client")
	}
	defer func() {
		if err := messagingClient.Close(); err != nil {
			log.WithError(err).Error("Failed to close messaging client")
		}
	}()

	// Initialize health checker
	healthChecker := health.NewChecker(storageClient, messagingClient)

	// Initialize upload handler
	uploadHandler := upload.NewHandler(cfg, storageClient, messagingClient, log)

	// Setup HTTP routes
	router := chi.NewRouter()

	// For now we focus only on authentication, we will add authorization later
	authMiddleware := auth.KubernetesAuthMiddleware(log)
	// API routes
	router.Route("/api/ingress/v1", func(r chi.Router) {
		r.Use(authMiddleware)
		r.Post("/upload", uploadHandler.HandleUpload)
	})

	// Health and observability routes
	router.Get("/health", healthChecker.Health)
	router.Get("/ready", healthChecker.Ready)
	router.With(authMiddleware).Get("/metrics", healthChecker.Metrics)

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.Server.IdleTimeout) * time.Second,
	}

	// Start server in a goroutine
	go func() {
		log.WithField("addr", server.Addr).Info("Starting HTTP server")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.WithError(err).Error("Server forced to shutdown")
	}

	log.Info("Server exited")
}
