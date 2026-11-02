package live

func decisionText(value string) string {
	switch value {
	case "approved":
		return "已批准"
	case "edited_approved":
		return "修改后批准"
	case "rejected":
		return "已拒绝"
	case "needs_info":
		return "需要补充信息"
	default:
		return value
	}
}
