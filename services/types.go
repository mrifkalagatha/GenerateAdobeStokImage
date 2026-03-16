package services

type GeneratedItem struct {
	Prompt  string
	ImageID string
	URL     string
}

type GenerateOptions struct {
	PassthroughPrompt  bool
	StrictSingleObject bool
	ModelID            string
	StyleUUID          string
	AspectRatio        string
	Seed               int
}

type MetadataSeed struct {
	Filename string
	Prompt   string
	FileType string
}

type AdobeStockMetadata struct {
	Filename string
	Title    string
	Keywords []string
	Category string
}
