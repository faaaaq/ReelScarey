-- The English-dub rule gains !-prefixed exclusion lines: a release matching an
-- exclusion never matches the rule, so a Dual Audio release naming another dub
-- language is rejected. Only installations still on the previous default rule
-- are upgraded, with the revision bumped like a saved edit; a customized rule
-- is left exactly as its owner saved it.
UPDATE anime_preferences
SET dub_rule = '/([\[\(][Ee]nglish[\W]?([aA-zZ ]+)?[Dd]ubs?|FuniDub|Dual( |-|.)?Audio|\[Dual\])/i
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
Dual-Audio';