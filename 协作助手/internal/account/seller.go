package account

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	RoleBuyer           = "buyer"
	RoleSeller          = "seller"
	RoleAdmin           = "admin"
	ApplicationPending  = "pending"
	ApplicationApproved = "approved"
	ApplicationRejected = "rejected"
)

var (
	ErrForbidden           = errors.New("当前账号无权执行此操作")
	ErrApplicationPending  = errors.New("已有待审核申请，请等待审核")
	ErrAlreadySeller       = errors.New("已是商家")
	ErrApplicationNotFound = errors.New("申请不存在")
	ErrApplicationReviewed = errors.New("这条申请已经审核，不能重复修改结果")
)

//go:embed applications.sql
var applicationSchemaSQL string

type sellerApplication struct {
	ID           int64 `gorm:"primaryKey"`
	UserID       int64
	Description  string
	Status       string
	CreatedAt    time.Time
	ReviewerID   *int64
	ReviewReason string
	ReviewedAt   *time.Time
}

func (sellerApplication) TableName() string { return "seller_applications" }

type Application struct {
	ID                int64      `json:"id"`
	ApplicantID       int64      `json:"applicant_id"`
	ApplicantUsername string     `json:"applicant_username"`
	ApplicantEmail    string     `json:"applicant_email"`
	Description       string     `json:"description"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
	ReviewerID        *int64     `json:"reviewer_id"`
	ReviewReason      string     `json:"review_reason"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
}

// ConfigureAdmin 在本地配置指定的账号上授予管理员身份。
// 0 表示暂不指定管理员；配置中的账号必须已存在且专用于管理。
func ConfigureAdmin(ctx context.Context, db *gorm.DB, userID int64) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if userID != 0 {
			var u user
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, userID).Error; err != nil {
				return fmt.Errorf("配置中的管理员账号必须已经注册: %w", err)
			}
			if u.Role == RoleSeller {
				return fmt.Errorf("管理员需使用专用账号，不能指定已是卖家的账号")
			}
		}
		// 配置是管理员授权来源，移除旧配置同时撤销旧管理员身份。
		if err := tx.Model(&user{}).Where("role = ? AND id <> ?", RoleAdmin, userID).Update("role", RoleBuyer).Error; err != nil {
			return err
		}
		if userID != 0 {
			return tx.Model(&user{}).Where("id = ?", userID).Update("role", RoleAdmin).Error
		}
		return nil
	})
}

func (s *Service) requireAdmin(ctx context.Context, userID int64) error {
	if userID != s.adminUserID || userID == 0 {
		return ErrForbidden
	}
	var u user
	if err := s.db.WithContext(ctx).Select("id", "role").First(&u, userID).Error; err != nil {
		return err
	}
	if u.Role != RoleAdmin {
		return ErrForbidden
	}
	return nil
}

func (s *Service) ApplySeller(ctx context.Context, userID int64, description string) (Application, error) {
	description = strings.TrimSpace(description)
	if description == "" || !utf8.ValidString(description) || len(description) > 65535 {
		return Application{}, &ValidationError{"申请说明不能为空，且不能超过 65535 个 UTF-8 字节"}
	}
	var u user
	var application sellerApplication
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 同一用户的检查与创建放在同一事务里，避免重复的待审核记录。
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, userID).Error; err != nil {
			return err
		}
		if u.Role == RoleSeller {
			return ErrAlreadySeller
		}
		if u.Role != RoleBuyer {
			return ErrForbidden
		}
		var count int64
		if err := tx.Model(&sellerApplication{}).Where("user_id = ? AND status = ?", userID, ApplicationPending).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrApplicationPending
		}
		application = sellerApplication{UserID: userID, Description: description, Status: ApplicationPending, CreatedAt: time.Now().UTC()}
		return tx.Create(&application).Error
	})
	return applicationResult(application, u), err
}

func (s *Service) MyApplications(ctx context.Context, userID int64) ([]Application, error) {
	return scanApplications(s.applicationQuery(ctx).Where("a.user_id = ?", userID))
}

func (s *Service) AdminApplications(ctx context.Context, userID int64) ([]Application, error) {
	if err := s.requireAdmin(ctx, userID); err != nil {
		return nil, err
	}
	return scanApplications(s.applicationQuery(ctx))
}

func (s *Service) ReviewApplication(ctx context.Context, reviewerID, applicationID int64, status, reason string) (Application, error) {
	if err := s.requireAdmin(ctx, reviewerID); err != nil {
		return Application{}, err
	}
	reason = strings.TrimSpace(reason)
	if status != ApplicationApproved && status != ApplicationRejected {
		return Application{}, &ValidationError{"审核状态只能是 approved 或 rejected"}
	}
	if !utf8.ValidString(reason) || len(reason) > 65535 || (status == ApplicationRejected && reason == "") {
		return Application{}, &ValidationError{"拒绝时必须填写理由，审核说明最多 65535 个 UTF-8 字节"}
	}
	var application sellerApplication
	var applicant user
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&application, applicationID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrApplicationNotFound
		}
		if err != nil {
			return err
		}
		if application.Status != ApplicationPending {
			return ErrApplicationReviewed
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&applicant, application.UserID).Error; err != nil {
			return err
		}
		if status == ApplicationApproved {
			if applicant.Role == RoleSeller {
				return ErrAlreadySeller
			}
			if applicant.Role != RoleBuyer {
				return ErrForbidden
			}
			if err := tx.Model(&applicant).Update("role", RoleSeller).Error; err != nil {
				return err
			}
		}
		now := time.Now().UTC()
		application.Status, application.ReviewReason = status, reason
		application.ReviewerID, application.ReviewedAt = &reviewerID, &now
		return tx.Model(&application).Updates(map[string]any{"status": status, "review_reason": reason, "reviewer_id": reviewerID, "reviewed_at": now}).Error
	})
	return applicationResult(application, applicant), err
}

func (s *Service) applicationQuery(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("seller_applications AS a").
		Select("a.id, a.user_id AS applicant_id, u.username AS applicant_username, u.email AS applicant_email, a.description, a.status, a.created_at, a.reviewer_id, a.review_reason, a.reviewed_at").
		Joins("JOIN users AS u ON u.id = a.user_id").Order("a.created_at DESC, a.id DESC")
}

func scanApplications(query *gorm.DB) ([]Application, error) {
	applications := make([]Application, 0)
	err := query.Scan(&applications).Error
	return applications, err
}

func applicationResult(a sellerApplication, u user) Application {
	return Application{ID: a.ID, ApplicantID: a.UserID, ApplicantUsername: u.Username, ApplicantEmail: u.Email, Description: a.Description, Status: a.Status, CreatedAt: a.CreatedAt, ReviewerID: a.ReviewerID, ReviewReason: a.ReviewReason, ReviewedAt: a.ReviewedAt}
}
