CREATE TABLE IF NOT EXISTS seller_applications (
    id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id INT NOT NULL,
    description TEXT NOT NULL,
    status ENUM('pending', 'approved', 'rejected') NOT NULL DEFAULT 'pending',
    created_at DATETIME(6) NOT NULL,
    reviewer_id INT NULL,
    review_reason TEXT NOT NULL,
    reviewed_at DATETIME(6) NULL,
    KEY ix_seller_applications_user_status (user_id, status),
    CONSTRAINT fk_seller_applications_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_seller_applications_reviewer FOREIGN KEY (reviewer_id) REFERENCES users(id)
);
