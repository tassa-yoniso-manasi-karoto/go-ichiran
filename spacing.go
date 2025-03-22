package ichiran

import (
	"strings"
	
	"github.com/tassa-yoniso-manasi-karoto/translitkit/common"
)

// JoinWithSpacingRule joins string slices using intelligent spacing rules
func JoinWithSpacingRule(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	
	if len(tokens) == 1 {
		return tokens[0]
	}
	
	var builder strings.Builder
	builder.WriteString(tokens[0])
	
	for i := 1; i < len(tokens); i++ {
		if common.DefaultSpacingRule(tokens[i-1], tokens[i]) {
			builder.WriteRune(' ')
		}
		builder.WriteString(tokens[i])
	}
	
	return builder.String()
}