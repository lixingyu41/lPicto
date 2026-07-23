package db

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

func filenameSortKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "#"
	}
	var builder strings.Builder
	builder.Grow(len(name))
	for _, r := range name {
		builder.WriteString(filenameSortToken(r))
	}
	key := strings.TrimSpace(builder.String())
	if key == "" {
		return "#"
	}
	return strings.ToLower(key)
}

func filenameSortToken(r rune) string {
	if r == '\u3000' {
		return " "
	}
	if r >= '！' && r <= '～' {
		r -= 0xFEE0
	}
	if r < utf8.RuneSelf {
		if unicode.IsLetter(r) {
			return string(unicode.ToLower(r))
		}
		if unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsSpace(r) {
			return string(r)
		}
		return "#"
	}
	if r >= 'Ａ' && r <= 'Ｚ' {
		return string('a' + (r - 'Ａ'))
	}
	if r >= 'ａ' && r <= 'ｚ' {
		return string('a' + (r - 'ａ'))
	}
	if r >= '０' && r <= '９' {
		return string('0' + (r - '０'))
	}
	if initial, ok := hanPinyinInitial(r); ok {
		return string(initial)
	}
	if initial, ok := kanaRomanInitial(r); ok {
		return string(initial)
	}
	if initial, ok := hangulRomanInitial(r); ok {
		return string(initial)
	}
	if initial, ok := greekRomanInitial(r); ok {
		return string(initial)
	}
	if initial, ok := cyrillicRomanInitial(r); ok {
		return string(initial)
	}
	if token := latinFoldToken(r); token != "" {
		return token
	}
	if unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return "#"
	}
	if unicode.IsLetter(r) {
		return "z"
	}
	return "#"
}

func hanPinyinInitial(r rune) (byte, bool) {
	if !unicode.In(r, unicode.Han) {
		return 0, false
	}
	encoded, _, err := transform.String(simplifiedchinese.GB18030.NewEncoder(), string(r))
	if err != nil || len(encoded) < 2 {
		return 0, false
	}
	code := int(int16(uint16(encoded[0])<<8 | uint16(encoded[1])))
	thresholds := []struct {
		code    int
		initial byte
	}{
		{-20319, 'a'},
		{-20283, 'b'},
		{-19775, 'c'},
		{-19218, 'd'},
		{-18710, 'e'},
		{-18526, 'f'},
		{-18239, 'g'},
		{-17922, 'h'},
		{-17417, 'j'},
		{-16474, 'k'},
		{-16212, 'l'},
		{-15640, 'm'},
		{-15165, 'n'},
		{-14922, 'o'},
		{-14914, 'p'},
		{-14630, 'q'},
		{-14149, 'r'},
		{-14090, 's'},
		{-13318, 't'},
		{-12838, 'w'},
		{-12556, 'x'},
		{-11847, 'y'},
		{-11055, 'z'},
	}
	if code < thresholds[0].code || code > -10247 {
		return 0, false
	}
	for index := len(thresholds) - 1; index >= 0; index-- {
		if code >= thresholds[index].code {
			return thresholds[index].initial, true
		}
	}
	return 0, false
}

func kanaRomanInitial(r rune) (byte, bool) {
	if r >= 'ァ' && r <= 'ヶ' {
		r -= 0x60
	}
	switch {
	case strings.ContainsRune("ぁあぃいぅうぇえぉおゔ", r):
		return 'a', true
	case strings.ContainsRune("かがきぎくぐけげこご", r):
		if strings.ContainsRune("がぎぐげご", r) {
			return 'g', true
		}
		return 'k', true
	case strings.ContainsRune("さざしじすずせぜそぞ", r):
		if strings.ContainsRune("ざじずぜぞ", r) {
			return 'z', true
		}
		return 's', true
	case strings.ContainsRune("ただちぢっつづてでとど", r):
		if strings.ContainsRune("だぢづでど", r) {
			return 'd', true
		}
		return 't', true
	case strings.ContainsRune("なにぬねの", r):
		return 'n', true
	case strings.ContainsRune("はばぱひびぴふぶぷへべぺほぼぽ", r):
		if strings.ContainsRune("ばびぶべぼ", r) {
			return 'b', true
		}
		if strings.ContainsRune("ぱぴぷぺぽ", r) {
			return 'p', true
		}
		return 'h', true
	case strings.ContainsRune("まみむめも", r):
		return 'm', true
	case strings.ContainsRune("ゃやゅゆょよ", r):
		return 'y', true
	case strings.ContainsRune("らりるれろ", r):
		return 'r', true
	case strings.ContainsRune("ゎわをん", r):
		return 'w', true
	default:
		return 0, false
	}
}

func hangulRomanInitial(r rune) (byte, bool) {
	if r >= 0xAC00 && r <= 0xD7A3 {
		initials := []byte{'g', 'k', 'n', 'd', 't', 'r', 'm', 'b', 'p', 's', 's', 'a', 'j', 'j', 'c', 'k', 't', 'p', 'h'}
		index := int(r-0xAC00) / 588
		return initials[index], true
	}
	switch {
	case strings.ContainsRune("ㄱㄲㅋ", r):
		return 'g', true
	case strings.ContainsRune("ㄴ", r):
		return 'n', true
	case strings.ContainsRune("ㄷㄸㅌ", r):
		return 'd', true
	case strings.ContainsRune("ㄹ", r):
		return 'r', true
	case strings.ContainsRune("ㅁ", r):
		return 'm', true
	case strings.ContainsRune("ㅂㅃㅍ", r):
		return 'b', true
	case strings.ContainsRune("ㅅㅆ", r):
		return 's', true
	case strings.ContainsRune("ㅇ", r):
		return 'a', true
	case strings.ContainsRune("ㅈㅉㅊ", r):
		return 'j', true
	case strings.ContainsRune("ㅎ", r):
		return 'h', true
	default:
		return 0, false
	}
}

func greekRomanInitial(r rune) (byte, bool) {
	switch unicode.ToLower(r) {
	case 'α':
		return 'a', true
	case 'β':
		return 'b', true
	case 'γ':
		return 'g', true
	case 'δ':
		return 'd', true
	case 'ε', 'η':
		return 'e', true
	case 'ζ':
		return 'z', true
	case 'θ', 'τ':
		return 't', true
	case 'ι':
		return 'i', true
	case 'κ':
		return 'k', true
	case 'λ':
		return 'l', true
	case 'μ':
		return 'm', true
	case 'ν':
		return 'n', true
	case 'ξ':
		return 'x', true
	case 'ο', 'ω':
		return 'o', true
	case 'π', 'ψ':
		return 'p', true
	case 'ρ':
		return 'r', true
	case 'σ', 'ς':
		return 's', true
	case 'υ':
		return 'u', true
	case 'φ':
		return 'f', true
	case 'χ':
		return 'c', true
	default:
		return 0, false
	}
}

func cyrillicRomanInitial(r rune) (byte, bool) {
	switch unicode.ToLower(r) {
	case 'а':
		return 'a', true
	case 'б':
		return 'b', true
	case 'в':
		return 'v', true
	case 'г':
		return 'g', true
	case 'д':
		return 'd', true
	case 'е', 'ё', 'э':
		return 'e', true
	case 'ж', 'з':
		return 'z', true
	case 'и', 'й':
		return 'i', true
	case 'к':
		return 'k', true
	case 'л':
		return 'l', true
	case 'м':
		return 'm', true
	case 'н':
		return 'n', true
	case 'о':
		return 'o', true
	case 'п':
		return 'p', true
	case 'р':
		return 'r', true
	case 'с':
		return 's', true
	case 'т':
		return 't', true
	case 'у':
		return 'u', true
	case 'ф':
		return 'f', true
	case 'х':
		return 'h', true
	case 'ц', 'ч':
		return 'c', true
	case 'ш', 'щ':
		return 's', true
	case 'ы':
		return 'y', true
	case 'ю', 'я':
		return 'y', true
	default:
		return 0, false
	}
}

func latinFoldToken(r rune) string {
	decomposed := norm.NFD.String(string(r))
	for _, folded := range decomposed {
		if unicode.Is(unicode.Mn, folded) {
			continue
		}
		if folded < utf8.RuneSelf {
			if unicode.IsLetter(folded) {
				return string(unicode.ToLower(folded))
			}
			if unicode.IsDigit(folded) || unicode.IsPunct(folded) || unicode.IsSymbol(folded) {
				return string(folded)
			}
		}
	}
	return ""
}

func assetFilenameSortKey(assetName string, storedKey string) string {
	if key := strings.TrimSpace(storedKey); key != "" {
		return strings.ToLower(key)
	}
	return filenameSortKey(assetName)
}

func filenameAnchorLabel(sortKey string) string {
	sortKey = strings.TrimSpace(sortKey)
	if sortKey == "" {
		return "#"
	}
	r, _ := utf8.DecodeRuneInString(sortKey)
	r = unicode.ToUpper(r)
	if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
		return string(r)
	}
	if r < utf8.RuneSelf && (unicode.IsPunct(r) || unicode.IsSymbol(r)) {
		return string(r)
	}
	return "#"
}

func (d *DB) BackfillFilenameSortKeys(ctx context.Context) error {
	const batchSize = 500
	for {
		rows, err := d.conn.QueryContext(ctx, `
SELECT id, basename
FROM media_asset
WHERE filename_sort_key IS NULL OR filename_sort_key = ''
ORDER BY id
LIMIT ?`, batchSize)
		if err != nil {
			return err
		}
		items := make([]struct {
			id       int64
			basename string
		}, 0, batchSize)
		for rows.Next() {
			var item struct {
				id       int64
				basename string
			}
			if err := rows.Scan(&item.id, &item.basename); err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, item)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		tx, err := d.conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `UPDATE media_asset SET filename_sort_key = ? WHERE id = ? AND (filename_sort_key IS NULL OR filename_sort_key = '')`, filenameSortKey(item.basename), item.id); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
}
