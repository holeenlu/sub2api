package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Task type constants
const (
	TaskTypeAdminSecurityChange = "admin_security_change"
	TaskTypeVerifyCode          = "verify_code"
	TaskTypePasswordReset       = "password_reset"
)

// EmailTask 邮件发送任务
type EmailTask struct {
	SecurityUserID  int64
	SecurityActorID int64
	Email           string
	SiteName        string
	TaskType        string // "verify_code" or "password_reset"
	ResetURL        string // Only used for password_reset task type
	Locale          string // Optional Accept-Language locale hint
}

// EmailQueueService 异步邮件队列服务
type EmailQueueService struct {
	workerCtx context.Context

	emailService queuedEmailSender
	taskChan     chan EmailTask
	workers      int
	mu           sync.Mutex // owns admission, channel closure and worker completion
	started      bool
	draining     bool
	remaining    int
	done         chan struct{}
	cancel       context.CancelFunc
	unfinished   int
}

// NewEmailQueueService 创建邮件队列服务
func NewEmailQueueService(emailService *EmailService, workers int) *EmailQueueService {
	if workers <= 0 {
		workers = 3 // 默认3个工作协程
	}

	service := &EmailQueueService{
		emailService: emailService,
		taskChan:     make(chan EmailTask, 100), // 缓冲100个任务

		workers: workers,
	}

	return service
}

// queuedEmailSender keeps delivery behavior in EmailService while the queue
// owns scheduling and cancellation. Construction never sends or starts workers.
type queuedEmailSender interface {
	SendVerifyCode(context.Context, string, string, ...string) error
	SendPasswordResetEmailWithCooldown(context.Context, string, string, string, ...string) error
	SendManagementSecurityChange(context.Context, string, string, int64, int64) error
}

// Start attaches workers to the process lifetime. Requests are admitted only
// after the composition root has started its dependencies.
func (s *EmailQueueService) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return errors.New("email queue stopped")
	}
	if s.started {
		return nil
	}
	workerCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.workerCtx = workerCtx
	s.started = true
	s.done = make(chan struct{})
	s.remaining = s.workers
	for i := 0; i < s.workers; i++ {
		go s.worker(workerCtx, i)
	}
	return nil
}

func (s *EmailQueueService) worker(ctx context.Context, id int) {
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.remaining--
		if s.remaining == 0 {
			close(s.done)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case task, ok := <-s.taskChan:
			if !ok {
				return
			}
			s.processTask(ctx, id, task)
			s.mu.Lock()
			if ctx.Err() == nil {
				s.unfinished--
			}
			s.mu.Unlock()
		}
	}
}

func (s *EmailQueueService) enqueue(task EmailTask) error {
	if s == nil {
		return errors.New("email queue unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.draining || (s.workerCtx != nil && s.workerCtx.Err() != nil) {
		return errors.New("email queue not accepting work")
	}
	select {
	case s.taskChan <- task:
		s.unfinished++
		return nil
	default:
		return errors.New("email queue is full")
	}
}

// Drain closes admission atomically, processes accepted mail in order and joins
// workers. A timeout retains the join handle; it never claims delivery succeeded.
func (s *EmailQueueService) Drain(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.draining {
		s.draining = true
		close(s.taskChan)
	}
	done := s.done
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	pending := s.unfinished
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if pending > 0 {
		return fmt.Errorf("email queue stopped with %d accepted tasks unfinished", pending)
	}
	return nil
}

// Stop cancels delivery then joins it. Pending in-memory mail is reported as
// unfinished, not silently treated as sent. The application supplies the deadline.
func (s *EmailQueueService) Stop(ctx context.Context) error {
	if s != nil {
		s.mu.Lock()
		stop := s.cancel
		s.mu.Unlock()
		if stop != nil {
			stop()
		}
	}
	return s.Drain(ctx)
}

// processTask 处理任务
func (s *EmailQueueService) processTask(parent context.Context, workerID int, task EmailTask) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	switch task.TaskType {
	case TaskTypeAdminSecurityChange:
		if err := s.emailService.SendManagementSecurityChange(ctx, task.Email, task.SiteName, task.SecurityUserID, task.SecurityActorID); err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] security notification failed: %v", err)
		}
	case TaskTypeVerifyCode:
		if err := s.emailService.SendVerifyCode(ctx, task.Email, task.SiteName, task.Locale); err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send verify code to %s: %v", workerID, task.Email, err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent verify code to %s", workerID, task.Email)
		}
	case TaskTypePasswordReset:
		if err := s.emailService.SendPasswordResetEmailWithCooldown(ctx, task.Email, task.SiteName, task.ResetURL, task.Locale); err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send password reset to %s: %v", workerID, task.Email, err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent password reset to %s", workerID, task.Email)
		}
	default:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d unknown task type: %s", workerID, task.TaskType)
	}
}

// EnqueueVerifyCode 将验证码发送任务加入队列
func (s *EmailQueueService) EnqueueVerifyCode(email, siteName string, locale ...string) error {
	task := EmailTask{
		Email:    email,
		SiteName: siteName,
		TaskType: TaskTypeVerifyCode,
		Locale:   firstEmailLocale(locale),
	}

	return s.enqueue(task)
}

// EnqueuePasswordReset 将密码重置邮件任务加入队列
func (s *EmailQueueService) EnqueuePasswordReset(email, siteName, resetURL string, locale ...string) error {
	task := EmailTask{
		Email:    email,
		SiteName: siteName,
		TaskType: TaskTypePasswordReset,
		ResetURL: resetURL,
		Locale:   firstEmailLocale(locale),
	}

	return s.enqueue(task)
}

// Reuse the bounded delivery queue without storing passwords, MFA seeds or tokens.
func (s *EmailQueueService) EnqueueManagementSecurityChange(email, siteName string, userID, actorID int64) error {
	return s.enqueue(EmailTask{Email: email, SiteName: siteName, TaskType: TaskTypeAdminSecurityChange, SecurityUserID: userID, SecurityActorID: actorID})
}
