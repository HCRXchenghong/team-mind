ALTER TABLE channel_accounts
  ADD COLUMN provider_base_url VARCHAR(255) NOT NULL DEFAULT 'https://ilinkai.weixin.qq.com' AFTER scanner_hash;
