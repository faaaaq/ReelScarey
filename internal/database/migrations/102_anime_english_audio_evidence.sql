-- The English-dub gate now also structurally requires explicit English-audio
-- evidence (quality.DubRule.Qualifies), so a bare "Dual Audio" label never
-- qualifies regardless of rule text. This migration additionally upgrades the
-- stored DEFAULT rule to recognize explicit "English Audio"/"Eng Audio"
-- labels, which previously matched nothing. Only installations still on the
-- 093 default rule are upgraded, with the revision bumped like a saved edit;
-- a customized rule is left exactly as its owner saved it (and still gains
-- the structural evidence gate).
UPDATE anime_preferences
SET dub_rule = '/([\[\(][Ee]nglish[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
/([\[\(][Ee]ng[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
Eng Dub
Eng Dubs
English Dub
English Dubs
English-Dub
English-Dubs
English Audio
Eng Audio
dual-audio
Dual-Audio
!/\b(french|fra|fre)\b/i
!/\b(spanish|spa|esp)\b/i
!/\b(portuguese|portugu[eê]s|pob|por[- ]br|por)\b/i
!/\b(german|ger|deu)\b/i
!/\b(italian|ita)\b/i
!/\b(korean|kor)\b/i
!/\b(chinese|mandarin|cantonese|chi|zho|zh)\b/i
!/\b(russian|rus)\b/i',
    revision = revision + 1,
    updated_at = unixepoch()
WHERE dub_rule = '/([\[\(][Ee]nglish[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
/([\[\(][Ee]ng[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
Eng Dub
Eng Dubs
English Dub
English Dubs
dual-audio
Dual-Audio
!/\b(french|fra|fre)\b/i
!/\b(spanish|spa|esp)\b/i
!/\b(portuguese|portugu[eê]s|pob|por[- ]br|por)\b/i
!/\b(german|ger|deu)\b/i
!/\b(italian|ita)\b/i
!/\b(korean|kor)\b/i
!/\b(chinese|mandarin|cantonese|chi|zho|zh)\b/i
!/\b(russian|rus)\b/i';
