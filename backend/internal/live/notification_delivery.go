package live

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// notifyRecipient applies inbox preferences at the point the business
// transaction writes its notification. Quiet hours defer visibility in the
// user's configured timezone; disabled notifications create no inbox row.
func notifyRecipient(tx *sql.Tx, tid, recipient, kind, title, body, resourceID string) error {
	return notifyResource(tx, tid, recipient, kind, title, body, "proposal", resourceID)
}

func notifyResource(tx *sql.Tx, tid, recipient, kind, title, body, resourceType, resourceID string) error {
	var teamArg any
	if tid != "" {
		teamArg = tid
	}
	deliveryID := id()
	dedupeKey := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", recipient, kind, resourceType, resourceID, "in_app", title, body)))
	result, err := tx.Exec(`INSERT IGNORE INTO notification_deliveries(id,team_id,recipient_user_id,notification_type,channel,status,idempotency_key) VALUES(UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),?,'in_app','queued',?)`, deliveryID, teamArg, teamArg, recipient, kind, dedupeKey[:])
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	var timezone string
	var enabled, emailEnabled, pushEnabled, nativePushEnabled bool
	var quietStart, quietEnd sql.NullString
	err = tx.QueryRow(`SELECT u.timezone,COALESCE(p.enabled,1),COALESCE(p.email_enabled,0),COALESCE(p.push_enabled,0),COALESCE(p.native_push_enabled,0),TIME_FORMAT(p.quiet_start,'%H:%i'),TIME_FORMAT(p.quiet_end,'%H:%i') FROM users u LEFT JOIN notification_preferences p ON p.user_id=u.id WHERE u.id=UNHEX(?) AND u.status='active'`, recipient).Scan(&timezone, &enabled, &emailEnabled, &pushEnabled, &nativePushEnabled, &quietStart, &quietEnd)
	if err == sql.ErrNoRows {
		_, err = tx.Exec(`UPDATE notification_deliveries SET status='suppressed',attempts=1,last_error='recipient_inactive' WHERE id=UNHEX(?)`, deliveryID)
		return err
	}
	if err != nil {
		return err
	}
	if !enabled {
		_, err = tx.Exec(`UPDATE notification_deliveries SET status='suppressed',attempts=1,last_error='user_disabled' WHERE id=UNHEX(?)`, deliveryID)
		return err
	}
	visibleAt := time.Now().UTC()
	if quietStart.Valid && quietEnd.Valid && quietStart.String != quietEnd.String {
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return err
		}
		local := visibleAt.In(loc)
		start, err := time.Parse("15:04", quietStart.String)
		if err != nil {
			return err
		}
		end, err := time.Parse("15:04", quietEnd.String)
		if err != nil {
			return err
		}
		startMinute := start.Hour()*60 + start.Minute()
		endMinute := end.Hour()*60 + end.Minute()
		minute := local.Hour()*60 + local.Minute()
		inQuiet := false
		if startMinute < endMinute {
			inQuiet = minute >= startMinute && minute < endMinute
		} else {
			inQuiet = minute >= startMinute || minute < endMinute
		}
		if inQuiet {
			endDay := local
			if startMinute > endMinute && minute >= startMinute {
				endDay = local.AddDate(0, 0, 1)
			}
			visibleAt = time.Date(endDay.Year(), endDay.Month(), endDay.Day(), end.Hour(), end.Minute(), 0, 0, loc).UTC()
		}
	}
	notificationID := id()
	_, err = tx.Exec(`INSERT INTO notifications(id,team_id,recipient_user_id,type,title,body,resource_type,resource_id,visible_at) VALUES(UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),?,?,?,?,UNHEX(?),?)`, notificationID, teamArg, teamArg, recipient, kind, title, body, resourceType, resourceID, visibleAt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE notification_deliveries SET notification_id=UNHEX(?),status='delivered',attempts=1,delivered_at=UTC_TIMESTAMP(3) WHERE id=UNHEX(?) AND status='queued'`, notificationID, deliveryID)
	if err != nil {
		return err
	}
	if emailEnabled {
		status, reason := "queued", any(nil)
		if !notificationEmailConfigured() {
			status, reason = "suppressed", "channel_not_configured"
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00email", recipient, kind, resourceType, resourceID)))
		_, err = tx.Exec(`INSERT IGNORE INTO notification_deliveries(id,notification_id,team_id,recipient_user_id,notification_type,channel,status,idempotency_key,next_retry_at,last_error,attempts) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),?,'email',?,?,?, ?,IF(?='suppressed',1,0))`, id(), notificationID, teamArg, teamArg, recipient, kind, status, digest[:], visibleAt, reason, status)
		if err != nil {
			return err
		}
	}
	if pushEnabled {
		if !notificationPushConfigured() {
			return insertSuppressedDelivery(tx, notificationID, teamArg, recipient, kind, resourceType, resourceID, "web_push", "channel_not_configured")
		}
		rows, queryErr := tx.Query(`SELECT HEX(id) FROM notification_push_subscriptions WHERE user_id=UNHEX(?) AND status='active' ORDER BY created_at`, recipient)
		if queryErr != nil {
			return queryErr
		}
		var subscriptions []string
		for rows.Next() {
			var subscriptionID string
			if queryErr = rows.Scan(&subscriptionID); queryErr != nil {
				rows.Close()
				return queryErr
			}
			subscriptions = append(subscriptions, subscriptionID)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return queryErr
		}
		if len(subscriptions) == 0 {
			if err = insertSuppressedDelivery(tx, notificationID, teamArg, recipient, kind, resourceType, resourceID, "web_push", "no_active_subscription"); err != nil {
				return err
			}
		}
		for _, subscriptionID := range subscriptions {
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00web_push\x00%s", recipient, kind, resourceType, resourceID, subscriptionID)))
			_, err = tx.Exec(`INSERT IGNORE INTO notification_deliveries(id,notification_id,team_id,recipient_user_id,subscription_id,notification_type,channel,status,idempotency_key,next_retry_at) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),UNHEX(?),?,'web_push','queued',?,?)`, id(), notificationID, teamArg, teamArg, recipient, subscriptionID, kind, digest[:], visibleAt)
			if err != nil {
				return err
			}
		}
	}
	if nativePushEnabled {
		if !nativePushConfigured() {
			return insertSuppressedDelivery(tx, notificationID, teamArg, recipient, kind, resourceType, resourceID, "native_push", "channel_not_configured")
		}
		rows, queryErr := tx.Query(`SELECT HEX(id) FROM notification_native_devices WHERE user_id=UNHEX(?) AND status='active' ORDER BY created_at`, recipient)
		if queryErr != nil {
			return queryErr
		}
		var devices []string
		for rows.Next() {
			var deviceID string
			if queryErr = rows.Scan(&deviceID); queryErr != nil {
				rows.Close()
				return queryErr
			}
			devices = append(devices, deviceID)
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return queryErr
		}
		if len(devices) == 0 {
			if err = insertSuppressedDelivery(tx, notificationID, teamArg, recipient, kind, resourceType, resourceID, "native_push", "no_active_device"); err != nil {
				return err
			}
		}
		for _, deviceID := range devices {
			digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00native_push\x00%s", recipient, kind, resourceType, resourceID, deviceID)))
			_, err = tx.Exec(`INSERT IGNORE INTO notification_deliveries(id,notification_id,team_id,recipient_user_id,native_device_id,notification_type,channel,status,idempotency_key,next_retry_at) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),UNHEX(?),?,'native_push','queued',?,?)`, id(), notificationID, teamArg, teamArg, recipient, deviceID, kind, digest[:], visibleAt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func insertSuppressedDelivery(tx *sql.Tx, notificationID string, teamArg any, recipient, kind, resourceType, resourceID, channel, reason string) error {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", recipient, kind, resourceType, resourceID, channel)))
	_, err := tx.Exec(`INSERT IGNORE INTO notification_deliveries(id,notification_id,team_id,recipient_user_id,notification_type,channel,status,idempotency_key,last_error,attempts) VALUES(UNHEX(?),UNHEX(?),IF(? IS NULL,NULL,UNHEX(?)),UNHEX(?),?,?, 'suppressed',?,?,1)`, id(), notificationID, teamArg, teamArg, recipient, kind, channel, digest[:], reason)
	return err
}
