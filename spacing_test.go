package ichiran

import (
	"fmt"
	"strings"
	"testing"
	
	"github.com/tassa-yoniso-manasi-karoto/translitkit/common"
)

func TestJoinWithSpacingRule(t *testing.T) {
	testCases := []struct {
		tokens   []string
		expected string
		desc     string
	}{
		{
			[]string{"私", "は", "日本語", "を", "勉強", "して", "います"},
			"私 は 日本語 を 勉強 して います",
			"Basic Japanese tokenization",
		},
		{
			[]string{"Hello", ",", "world", "!"},
			"Hello, world!",
			"English with punctuation",
		},
		{
			[]string{"(", "これ", "は", "テスト", "です", ")"},
			"(これ は テスト です)",
			"Japanese with parentheses",
		},
		{
			[]string{"1", "+", "2", "=", "3"},
			"1+2=3",
			"Math expression",
		},
	}

	for _, tc := range testCases {
		result := JoinWithSpacingRule(tc.tokens)
		if result != tc.expected {
			t.Errorf("JoinWithSpacingRule: expected '%s', got '%s' (%s)",
				tc.expected, result, tc.desc)
		}
	}
}

func TestCompareSpacingMethods(t *testing.T) {
	testTokens := []string{
		"私", "は", "日本語", "を", "勉強", "して", "います", "。",
		"毎日", "、", "新しい", "単語", "と", "文法", "を", "学んで", "います", "。",
	}

	simpleJoin := strings.Join(testTokens, " ")
	smartJoin := JoinWithSpacingRule(testTokens)

	fmt.Println("Simple Join:", simpleJoin)
	fmt.Println("Smart Join:", smartJoin)

	// Compare with expected results
	expectedSimple := "私 は 日本語 を 勉強 して います 。 毎日 、 新しい 単語 と 文法 を 学んで います 。"
	expectedSmart := "私 は 日本語 を 勉強 して います。毎日、新しい 単語 と 文法 を 学んで います。"

	if simpleJoin != expectedSimple {
		t.Errorf("Simple join: expected '%s', got '%s'", expectedSimple, simpleJoin)
	}

	if smartJoin != expectedSmart {
		t.Errorf("Smart join: expected '%s', got '%s'", expectedSmart, smartJoin)
	}
}

// A simple test to verify that our common.DefaultSpacingRule is being used correctly
func TestDefaultSpacingRuleImport(t *testing.T) {
	testCases := []struct {
		prev     string
		current  string
		expected bool
	}{
		{"日本", "語", true},      // Japanese characters should have space
		{"Hello", ",", false},   // No space before comma
		{"私", "は", true},        // Space between Japanese characters
		{"は", "、", false},       // No space before Japanese comma
	}

	for _, tc := range testCases {
		result := common.DefaultSpacingRule(tc.prev, tc.current)
		if result != tc.expected {
			t.Errorf("common.DefaultSpacingRule for '%s' and '%s': expected %v, got %v",
				tc.prev, tc.current, tc.expected, result)
		}
	}
}

func BenchmarkSimpleJoin(b *testing.B) {
	testTokens := []string{
		"私", "は", "日本語", "を", "勉強", "して", "います", "。",
		"毎日", "、", "新しい", "単語", "と", "文法", "を", "学んで", "います", "。",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strings.Join(testTokens, " ")
	}
}

func BenchmarkSmartJoin(b *testing.B) {
	testTokens := []string{
		"私", "は", "日本語", "を", "勉強", "して", "います", "。",
		"毎日", "、", "新しい", "単語", "と", "文法", "を", "学んで", "います", "。",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = JoinWithSpacingRule(testTokens)
	}
}