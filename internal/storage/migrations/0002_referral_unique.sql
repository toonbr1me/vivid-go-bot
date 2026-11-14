-- Приглашение привязывает приглашённого ровно к одному пригласившему.
--
-- Раньше уникальность была только на старой колонке referred_id (Telegram-id),
-- которая у новых записей пустая. Поэтому повторный /start или гонка двух
-- запросов заводили вторую строку, а счётчик награды увеличивался дважды:
-- клиент получал и дни, и деньги дважды.
--
-- Сначала сливаем счётчики в самую раннюю строку, потом удаляем дубли и
-- запрещаем новые.

UPDATE referrals r SET
	reward_count = merged.reward_count,
	bonus_days = merged.bonus_days
FROM (
	SELECT referred_subject_id,
	       max(reward_count) AS reward_count,
	       max(bonus_days) AS bonus_days
	FROM referrals
	WHERE referred_subject_id IS NOT NULL
	GROUP BY referred_subject_id
) merged
WHERE r.referred_subject_id = merged.referred_subject_id
  AND r.id = (
	SELECT min(x.id) FROM referrals x WHERE x.referred_subject_id = r.referred_subject_id
  );

DELETE FROM referrals r
USING referrals other
WHERE r.referred_subject_id = other.referred_subject_id
  AND r.referred_subject_id IS NOT NULL
  AND r.id > other.id;

CREATE UNIQUE INDEX IF NOT EXISTS referrals_referred_subject_uniq
	ON referrals (referred_subject_id)
	WHERE referred_subject_id IS NOT NULL;
