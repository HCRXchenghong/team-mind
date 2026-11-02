ALTER TABLE platform_settings
  DROP COLUMN public_url, DROP COLUMN smtp_host, DROP COLUMN smtp_port,
  DROP COLUMN smtp_user, DROP COLUMN smtp_password_ciphertext,
  DROP COLUMN smtp_from, DROP COLUMN smtp_tls_insecure,
  DROP COLUMN stt_provider, DROP COLUMN stt_base_url,
  DROP COLUMN stt_api_key_ciphertext, DROP COLUMN stt_model,
  DROP COLUMN stt_language, DROP COLUMN vapid_subject,
  DROP COLUMN wechat_mode, DROP COLUMN wechat_gateway_url,
  DROP COLUMN wechat_gateway_bridge_token_ciphertext,
  DROP COLUMN updated_by;
