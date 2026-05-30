package cfst

import (
	"context"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/service"
)

var _ adapter.LifecycleService = (*CFSTService)(nil)
var _ adapter.CFSTService = (*CFSTService)(nil)

// CFSTService manages CFST speed tests and dynamic outbound generation.
type CFSTService struct {
	ctx     context.Context
	logger  log.ContextLogger
	options option.CFSTOptions

	outbound adapter.OutboundManager
	router   adapter.Router

	mu            sync.Mutex
	running       bool
	cancel        context.CancelFunc
	results       []Result
	lastRun       time.Time
	lastError     string
	generatedTags map[string][]string // base tag -> generated outbound tags
	groupTags     map[string]string   // base tag -> urltest group tag
}

// NewService creates a new CFST service.
func NewService(ctx context.Context, logger log.ContextLogger, options option.CFSTOptions) *CFSTService {
	return &CFSTService{
		ctx:           ctx,
		logger:        logger,
		options:       options,
		generatedTags: make(map[string][]string),
		groupTags:     make(map[string]string),
	}
}

func (s *CFSTService) Name() string {
	return "cfst"
}

func (s *CFSTService) Start(stage adapter.StartStage) error {
	switch stage {
	case adapter.StartStatePostStart:
		s.outbound = service.FromContext[adapter.OutboundManager](s.ctx)
		s.router = service.FromContext[adapter.Router](s.ctx)

		// Load cache and generate outbounds from cached results
		if s.options.CacheFile != "" {
			results, err := LoadCache(s.options.CacheFile)
			if err == nil && len(results) > 0 {
				s.logger.Info("loaded ", len(results), " cached results")
				s.mu.Lock()
				s.results = results
				s.mu.Unlock()
				s.GenerateAllOutbounds(results)
			}
		}

		// Optionally run speed test on start
		if s.options.RunOnStart {
			s.mu.Lock()
			s.running = true
			s.mu.Unlock()

			dn := s.options.DownloadCount
			if dn <= 0 {
				dn = 10
			}
			p := s.options.DisplayCount
			if p <= 0 {
				p = 10
			}
			go s.runSpeedTest(dn, p)
		}
	}
	return nil
}

func (s *CFSTService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

// runSpeedTest executes the speed test in a goroutine.
func (s *CFSTService) runSpeedTest(downloadCount, displayCount int) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.mu.Unlock()
	}()

	s.logger.Info("starting speed test (dn=", downloadCount, ", p=", displayCount, ")")
	results, err := RunSpeedTest(ctx, downloadCount, displayCount)
	if err != nil {
		s.logger.Warn("speed test failed: ", err)
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		return
	}

	s.mu.Lock()
	s.results = results
	s.lastRun = time.Now()
	s.lastError = ""
	s.mu.Unlock()

	s.logger.Info("speed test completed, ", len(results), " results")

	// Save cache
	if s.options.CacheFile != "" {
		err = SaveCache(s.options.CacheFile, results)
		if err != nil {
			s.logger.Warn("failed to save cache: ", err)
		}
	}

	// Regenerate outbounds
	s.GenerateAllOutbounds(results)
}
