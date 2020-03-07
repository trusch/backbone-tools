package sqlizers

import (
	"sort"
	"strings"
)

type JSONContains map[string]interface{}

func (s JSONContains) ToSql() (sql string, args []interface{}, err error) {
	res := &strings.Builder{}
	_, _ = res.WriteRune('(')
	for i, k := range getSortedKeys(s) {
		if i > 0 {
			res.WriteString(" AND ")
		}
		_, _ = res.WriteString(k)
		_, _ = res.WriteString(" @> ?")
		args = append(args, s[k])
	}
	_, _ = res.WriteRune(')')
	sql = res.String()
	return sql, args, nil
}

func getSortedKeys(m map[string]interface{}) (res []string) {
	for k := range m {
		res = append(res, k)
	}
	sort.Strings(res)
	return res
}
