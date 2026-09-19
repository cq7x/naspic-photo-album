-- ============================================================
-- Naspic MySQL 初始化脚本
--
-- 只在 mysql 数据卷为空（首次启动）时执行一次。
-- 表结构本身由 Go 服务的 AutoMigrate 自动创建/补齐，
-- 这里只负责：建库、字符集、业务账号授权。
-- ============================================================

CREATE DATABASE IF NOT EXISTS `naspic`
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

-- 业务账号（与 compose 里的 MYSQL_USER / MYSQL_PASSWORD 对应）
-- 若账号已存在则只改密码，避免重复启动报错
CREATE USER IF NOT EXISTS 'naspic'@'%' IDENTIFIED BY 'naspic_2026';
ALTER USER 'naspic'@'%' IDENTIFIED BY 'naspic_2026';
GRANT ALL PRIVILEGES ON `naspic`.* TO 'naspic'@'%';

FLUSH PRIVILEGES;
