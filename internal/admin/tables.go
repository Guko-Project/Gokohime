package admin

type BotDataTable struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

func botDataTables() []BotDataTable {
	return []BotDataTable{
		{Name: "daily_lucks", Label: "今日运势", Description: "用户每日运势结果"},
		{Name: "daily_luck_templates", Label: "运势模板", Description: "今日运势文本和图片模板"},
		{Name: "cp_stories", Label: "CP 故事", Description: "CP 短打故事模板"},
		{Name: "meal_entries", Label: "吃什么", Description: "餐食/零食候选项"},
		{Name: "ktv_songs", Label: "KTV 歌曲", Description: "KTV 随机推荐曲库"},
		{Name: "guess_song_catalogs", Label: "猜歌曲库", Description: "猜歌歌曲 ID 与元数据"},
		{Name: "rand_pic_items", Label: "随机图片", Description: "随机图片分类与文件索引"},
		{Name: "plugin_kvs", Label: "插件 KV", Description: "插件轻量状态与配置"},
		{Name: "plugin_data_files", Label: "迁移 JSON", Description: "迁移导入的 JSON 原始数据"},
		{Name: "plugin_binary_files", Label: "迁移文件", Description: "迁移导入的二进制文件"},
		{Name: "migration_runs", Label: "迁移记录", Description: "数据迁移执行历史"},
	}
}

func botDataTableByName(name string) (BotDataTable, bool) {
	for _, table := range botDataTables() {
		if table.Name == name {
			return table, true
		}
	}
	return BotDataTable{}, false
}
