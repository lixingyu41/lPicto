WITH part_alias(canonical, alias) AS (
  VALUES
    ('脸部','脸部'),('脸部','面部'),('头部','头部'),('头部','头发'),
    ('眼部','眼部'),('眼部','眼睛'),('鼻部','鼻部'),('鼻部','鼻子'),
    ('嘴部','嘴部'),('嘴部','口部'),('嘴部','口腔'),('嘴唇','嘴唇'),('嘴唇','唇部'),
    ('舌部','舌部'),('舌部','舌头'),('牙齿','牙齿'),('耳部','耳部'),('耳部','耳朵'),
    ('颈部','颈部'),('颈部','脖子'),('肩部','肩部'),('肩部','肩膀'),('锁骨','锁骨'),
    ('胸部','胸部'),('腹部','腹部'),('腹部','肚子'),('腹部','腰腹'),('肚脐','肚脐'),
    ('腰部','腰部'),('背部','背部'),('手部','手部'),('手掌','手掌'),('手指','手指'),
    ('手臂','手臂'),('手臂','胳膊'),('肘部','肘部'),('肘部','手肘'),
    ('手腕','手腕'),('手腕','腕部'),('臀部','臀部'),('腿部','腿部'),
    ('大腿','大腿'),('膝部','膝部'),('膝部','膝盖'),('小腿','小腿'),
    ('脚踝','脚踝'),('脚踝','踝部'),('脚部','脚部'),('脚部','足部'),
    ('脚底','脚底'),('脚底','足底'),('脚趾','脚趾'),('脚趾','足趾'),('全身','全身')
),
matches AS (
  SELECT DISTINCT r.asset_id, p.canonical
  FROM asset_ai_result r
  JOIN part_alias p ON
    r.description LIKE '%' || p.alias || '特写%'
    OR r.description LIKE '%' || p.alias || '的特写%'
    OR r.description LIKE '%特写' || p.alias || '%'
  WHERE r.status='ready'
    AND r.description NOT LIKE '%未见' || p.alias || '特写%'
    AND r.description NOT LIKE '%没有' || p.alias || '特写%'
    AND r.description NOT LIKE '%无' || p.alias || '特写%'
)
INSERT INTO asset_ai_tag(
  asset_id, tag, confidence, category_key, category_label, subject_key, subject_label
)
SELECT m.asset_id, m.canonical || '特写', 1.0, 'closeup', '特写', 'part', '部位'
FROM matches m
WHERE (SELECT COUNT(*) FROM asset_ai_tag t WHERE t.asset_id=m.asset_id) < 10
ON CONFLICT(asset_id, tag) DO NOTHING;

WITH part_alias(canonical, alias) AS (
  VALUES
    ('脸部','脸部'),('脸部','面部'),('头部','头部'),('头部','头发'),
    ('眼部','眼部'),('眼部','眼睛'),('鼻部','鼻部'),('鼻部','鼻子'),
    ('嘴部','嘴部'),('嘴部','口部'),('嘴部','口腔'),('嘴唇','嘴唇'),('嘴唇','唇部'),
    ('舌部','舌部'),('舌部','舌头'),('牙齿','牙齿'),('耳部','耳部'),('耳部','耳朵'),
    ('颈部','颈部'),('颈部','脖子'),('肩部','肩部'),('肩部','肩膀'),('锁骨','锁骨'),
    ('胸部','胸部'),('腹部','腹部'),('腹部','肚子'),('腹部','腰腹'),('肚脐','肚脐'),
    ('腰部','腰部'),('背部','背部'),('手部','手部'),('手掌','手掌'),('手指','手指'),
    ('手臂','手臂'),('手臂','胳膊'),('肘部','肘部'),('肘部','手肘'),
    ('手腕','手腕'),('手腕','腕部'),('臀部','臀部'),('腿部','腿部'),
    ('大腿','大腿'),('膝部','膝部'),('膝部','膝盖'),('小腿','小腿'),
    ('脚踝','脚踝'),('脚踝','踝部'),('脚部','脚部'),('脚部','足部'),
    ('脚底','脚底'),('脚底','足底'),('脚趾','脚趾'),('脚趾','足趾'),('全身','全身')
),
matches AS (
  SELECT DISTINCT r.asset_id, p.canonical
  FROM asset_ai_result r
  JOIN part_alias p ON
    r.description LIKE '%' || p.alias || '特写%'
    OR r.description LIKE '%' || p.alias || '的特写%'
    OR r.description LIKE '%特写' || p.alias || '%'
  WHERE r.status='ready'
    AND r.description NOT LIKE '%未见' || p.alias || '特写%'
    AND r.description NOT LIKE '%没有' || p.alias || '特写%'
    AND r.description NOT LIKE '%无' || p.alias || '特写%'
)
INSERT INTO asset_ai_tag_facet(asset_id, tag, facet_key, node_id, node_ids, labels)
SELECT
  m.asset_id,
  m.canonical || '特写',
  'closeup.part.type',
  'ai:closeup.part.type:' || m.canonical,
  ARRAY['ai:closeup','ai:closeup.part','ai:closeup.part.type','ai:closeup.part.type:' || m.canonical],
  ARRAY['特写','部位','类型',m.canonical]
FROM matches m
JOIN asset_ai_tag t ON t.asset_id=m.asset_id AND t.tag=m.canonical || '特写'
ON CONFLICT DO NOTHING;
