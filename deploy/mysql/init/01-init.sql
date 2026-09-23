-- ============================================================
-- Naspic MySQL 初始化脚本
--
-- 只在 mysql 数据卷为空（首次启动）时执行一次。
-- 表结构本身由 Go 服务的 AutoMigrate 自动创建/补齐，
-- 这里只负责：建库、字符集。
-- 业务账号由 MySQL 镜像根据 MYSQL_USER/MYSQL_PASSWORD 环境变量自动创建，
-- 切勿在此处 ALTER USER 改密码，否则会覆盖 .env 里的随机密码！
-- ============================================================

CREATE DATABASE IF NOT EXISTS `naspic`
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

-- 确保 naspic 用户有完整权限（用户本身由镜像自动创建）
GRANT ALL PRIVILEGES ON `naspic`.* TO 'naspic'@'%';

FLUSH PRIVILEGES;
