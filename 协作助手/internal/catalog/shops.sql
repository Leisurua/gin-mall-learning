CREATE TABLE IF NOT EXISTS shops (
    id INT NOT NULL AUTO_INCREMENT PRIMARY KEY,
    owner_id INT NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL,
    updated_at DATETIME(6) NOT NULL,
    CONSTRAINT fk_shops_owner FOREIGN KEY (owner_id) REFERENCES users(id)
);
