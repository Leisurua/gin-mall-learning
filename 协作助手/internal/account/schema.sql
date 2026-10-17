CREATE TABLE IF NOT EXISTS users (
    id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    email VARCHAR(254) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    password_hash VARCHAR(60) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    role ENUM('buyer', 'seller', 'admin') NOT NULL DEFAULT 'buyer',
    UNIQUE KEY ux_users_username (username),
    UNIQUE KEY ux_users_email (email)
);
