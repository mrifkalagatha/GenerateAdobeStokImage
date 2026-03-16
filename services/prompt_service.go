package services

import (
	"log"
	"math/rand"
	"strings"
	"time"

	"ai-generator/utils"
)

type PromptService struct {
	logger *log.Logger
	rng    *rand.Rand
}

type StructuredPromptInput struct {
	Niche         string
	SubNiche      []string
	Style         string
	Composition   string
	Environment   []string
	Background    string
	ColorPalette  []string
	Lighting      string
	Complexity    string
	DetailLevel   string
	CommercialUse string
	AspectRatio   string
}

func NewPromptService(logger *log.Logger) *PromptService {
	return &PromptService{
		logger: logger,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (ps *PromptService) GenerateFromPrompts(prompts []string, minVariants, maxVariants int) []string {
	utils.LogFlow(ps.logger, "[PRM]", "VARIANT", "start base_prompts=%d min=%d max=%d", len(prompts), minVariants, maxVariants)

	if minVariants < 1 {
		minVariants = 1
	}
	if maxVariants < minVariants {
		maxVariants = minVariants
	}

	synonyms := map[string][]string{
		"business": {"corporate", "professional", "executive"},
		"modern":   {"contemporary", "sleek", "minimal"},
		"office":   {"workspace", "studio", "headquarters"},
	}
	defaultCameraAngles := []string{
		"wide-angle perspective", "eye-level shot", "high-angle shot", "close-up frame",
	}
	defaultLightingStyles := []string{
		"soft natural light", "cinematic rim light", "balanced studio lighting", "golden hour illumination",
	}
	defaultCompositions := []string{
		"rule of thirds composition", "clean centered composition", "dynamic leading lines", "layered depth composition",
	}
	defaultPhotoStyles := []string{
		"professional photography style", "stock photography style", "high-detail editorial style",
	}

	techCameraAngles := []string{
		"front centered framing", "symmetrical composition", "isometric perspective", "wide cinematic frame",
	}
	techLightingStyles := []string{
		"neon cyan violet glow", "volumetric futuristic lighting", "soft atmospheric bloom", "subtle reflective highlights",
	}
	techCompositions := []string{
		"single object center composition", "minimal negative space", "clean copy space for text overlay", "balanced geometric layout",
	}
	techStyles := []string{
		"abstract 3d render style", "futuristic digital art style", "commercial stock background style", "high detail clean render style",
	}

	variants := make([]string, 0, len(prompts)*maxVariants)
	seen := make(map[string]struct{})

	for _, base := range prompts {
		base = ensureNoHumansToken(strings.TrimSpace(strings.ToLower(base)))
		if base == "" {
			continue
		}

		cameraAngles := defaultCameraAngles
		lightingStyles := defaultLightingStyles
		compositions := defaultCompositions
		photoStyles := defaultPhotoStyles
		if isFuturisticTechNiche(base) {
			cameraAngles = techCameraAngles
			lightingStyles = techLightingStyles
			compositions = techCompositions
			photoStyles = techStyles
		}

		targetCount := minVariants
		if maxVariants > minVariants {
			targetCount = minVariants + ps.rng.Intn(maxVariants-minVariants+1)
		}

		generated := 0
		attempt := 0
		for generated < targetCount && attempt < targetCount*10 {
			attempt++
			keyword := pickSynonym(ps.rng, base, synonyms)
			variant := strings.Join([]string{
				keyword,
				randomFrom(ps.rng, cameraAngles),
				randomFrom(ps.rng, lightingStyles),
				randomFrom(ps.rng, compositions),
				randomFrom(ps.rng, photoStyles),
			}, ", ")
			variant = ensureNoHumansToken(variant)

			if _, ok := seen[variant]; ok {
				continue
			}
			seen[variant] = struct{}{}
			variants = append(variants, variant)
			generated++
		}

		utils.LogFlow(ps.logger, "[PRM]", "VARIANT", "base=%s generated=%d", base, generated)
	}

	utils.LogSuccess(ps.logger, "VARIANT", "done total_variants=%d", len(variants))
	return variants
}

func pickSynonym(rng *rand.Rand, prompt string, synonyms map[string][]string) string {
	parts := strings.Fields(prompt)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if syn, ok := synonyms[p]; ok && len(syn) > 0 {
			out = append(out, syn[rng.Intn(len(syn))])
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}

func randomFrom(rng *rand.Rand, in []string) string {
	if len(in) == 0 {
		return ""
	}
	return in[rng.Intn(len(in))]
}

func (ps *PromptService) BuildStructuredPrompt(input StructuredPromptInput) string {
	parts := make([]string, 0, 24)
	heroMode := isMysticalTechHeroNiche(input.Niche, input.CommercialUse)

	normalizedNiche := strings.TrimSpace(input.Niche)
	if isFuturisticTechNiche(normalizedNiche) {
		motifs := mysticalHeroMotifs()
		parts = append(parts, "abstract futuristic technology background")
		parts = append(parts, randomFrom(ps.rng, motifs))
	}

	if v := strings.TrimSpace(input.Niche); v != "" {
		parts = append(parts, v)
	}
	if len(input.SubNiche) > 0 {
		parts = append(parts, normalizeSubNiche(randomNonEmpty(ps.rng, input.SubNiche), heroMode))
	}
	if v := strings.TrimSpace(input.Style); v != "" {
		parts = append(parts, v+" style")
	}
	if v := strings.TrimSpace(input.Composition); v != "" {
		parts = append(parts, v+" composition")
	}
	if len(input.Environment) > 0 {
		parts = append(parts, normalizeEnvironment(randomNonEmpty(ps.rng, input.Environment), heroMode))
	}
	if v := strings.TrimSpace(input.Background); v != "" {
		parts = append(parts, normalizeBackground(v, heroMode)+" background")
	}
	if len(input.ColorPalette) > 0 {
		parts = append(parts, strings.Join(filterNonEmpty(input.ColorPalette), " ")+" color palette")
	}
	if v := strings.TrimSpace(input.Lighting); v != "" {
		parts = append(parts, v)
	}

	switch strings.ToLower(strings.TrimSpace(input.Complexity)) {
	case "low":
		parts = append(parts, "minimal details", "clean shapes")
	case "medium":
		parts = append(parts, "balanced details")
	case "high":
		parts = append(parts, "ultra detailed", "complex textures")
	}

	switch strings.ToLower(strings.TrimSpace(input.DetailLevel)) {
	case "low":
		parts = append(parts, "soft simplified detailing")
	case "medium":
		parts = append(parts, "refined details")
	case "ultra", "high":
		parts = append(parts, "ultra detailed", "premium surface detail")
	}

	if v := strings.TrimSpace(input.CommercialUse); v != "" {
		parts = append(parts, v)
	}
	if v := strings.TrimSpace(input.AspectRatio); v != "" {
		parts = append(parts, v+" framing")
	}

	if heroMode {
		parts = append(parts,
			"single isolated floating hero object",
			"premium luxury studio scene",
			"dark clean gradient backdrop",
			"subtle reflective surface",
			"symmetrical centered hero composition",
			"high commercial stock appeal",
		)
	}

	parts = append(parts,
		"cinematic lighting",
		"professional stock aesthetic",
		"adobe stock commercial safe",
		"clean copy space",
		"no humans",
		"no text",
		"no logo",
		"no watermark",
	)

	out := strings.Join(parts, ", ")
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func (ps *PromptService) OptimizeCommercialPrompts(niche string, subNiche []string, prompts []string, strictSingleObject bool) []string {
	out := make([]string, 0, len(prompts))
	for i, p := range prompts {
		pp := strings.TrimSpace(p)
		if pp == "" {
			continue
		}
		out = append(out, ps.hardenPromptForNiche(niche, subNiche, pp, i, strictSingleObject))
	}
	return out
}

func (ps *PromptService) hardenPromptForNiche(niche string, subNiche []string, base string, idx int, strictSingleObject bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	if !isMysticalTechHeroNiche(niche, "adobe stock hero image") && !isFuturisticTechNiche(niche) {
		if strictSingleObject {
			base = base + ", single object only, one subject, no duplicate objects, no object fragments"
		}
		return ensureNoHumansToken(base)
	}
	if isStrongCommercialBasePrompt(base) {
		return strengthenWithoutOverwriting(base, strictSingleObject)
	}

	lower := strings.ToLower(base)
	subject := pickHeroSubject(lower, subNiche)
	palette := "cyan blue violet"
	if hasAnyToken(lower, "magenta") {
		palette = "cyan violet soft magenta"
	}
	sceneTemplates := []string{
		"centered product-shot composition, object occupies 35 percent frame, crisp contour edges, premium micro-detail, high-end UI hero background",
		"symmetrical front view, object above reflective black glass pedestal, subtle cinematic mist, generous copy space left and right for banner usage",
		"isometric luxury render, controlled rim highlights, studio-grade shadow falloff, web hero section ready, clean negative space",
	}
	scene := sceneTemplates[idx%len(sceneTemplates)]

	parts := []string{
		subject,
		"single centered hero object",
		"isolated subject",
		"dark luxury gradient background",
		"subtle reflective surface",
		"cinematic volumetric lighting",
		"premium 3d render",
		"clean negative space",
		scene,
		"high commercial stock appeal",
		palette + " color palette",
		"no humans",
		"no text",
		"no logo",
		"no watermark",
	}
	if strictSingleObject {
		parts = append(parts,
			"single object only",
			"exactly one subject",
			"no duplicated object",
			"no object fragments",
			"no cluster",
		)
	}

	// Keep only hard business intent keywords from upstream prompt.
	baseIntent := extractCommercialIntent(base)
	if baseIntent != "" {
		parts = append([]string{baseIntent}, parts...)
	}

	out := strings.Join(parts, ", ")
	out = strings.Join(strings.Fields(out), " ")
	return out
}

func isStrongCommercialBasePrompt(base string) bool {
	l := strings.ToLower(base)
	subject := hasAnyToken(l, "crystal", "core", "sphere", "lotus", "cube", "artifact")
	composition := hasAnyToken(l, "floating", "reflective", "dark", "surface", "center", "single")
	light := hasAnyToken(l, "cinematic", "volumetric", "rim light", "soft light", "cyan", "violet")
	return subject && composition && light
}

func strengthenWithoutOverwriting(base string, strictSingleObject bool) string {
	subject := "single centered tech object"
	l := strings.ToLower(base)
	switch {
	case strings.Contains(l, "crystal"), strings.Contains(l, "gem"):
		subject = "single centered faceted crystal core (octahedral gemstone form)"
	case strings.Contains(l, "sphere"), strings.Contains(l, "orb"):
		subject = "single centered polished energy sphere"
	case strings.Contains(l, "cube"):
		subject = "single centered transparent holographic cube"
	case strings.Contains(l, "lotus"):
		subject = "single centered translucent digital lotus"
	}

	parts := []string{
		strings.TrimSpace(base),
		"commercial stock ready",
		subject,
		"one object only",
		"clean negative space",
		"premium object silhouette",
		"product-style hero composition",
		"physically based rendering",
		"ray-traced reflections and caustics",
		"sharp focus on subject",
		"high-frequency micro detail",
		"no humans",
		"no text",
		"no logo",
		"no watermark",
	}
	if strictSingleObject {
		parts = append(parts,
			"exactly one subject",
			"no duplicated object",
			"no object fragments",
			"no cluster",
		)
	}
	out := strings.Join(parts, ", ")
	return strings.Join(strings.Fields(out), " ")
}

func pickHeroSubject(promptLower string, subNiche []string) string {
	candidates := append([]string{}, subNiche...)
	candidates = append(candidates, promptLower)
	full := strings.ToLower(strings.Join(candidates, " | "))

	switch {
	case strings.Contains(full, "lotus"):
		return "single translucent digital lotus with luminous glass petals"
	case strings.Contains(full, "cube"):
		return "single transparent holographic cube with precise neon edges"
	case strings.Contains(full, "sphere"), strings.Contains(full, "orb"):
		return "single floating energy sphere with elegant halo rings"
	case strings.Contains(full, "artifact"), strings.Contains(full, "core"), strings.Contains(full, "crystal"):
		return "single floating glowing crystal core with internal energy veins"
	default:
		return "single floating futuristic tech crystal object"
	}
}

func suppressWeakDescriptors(in string) string {
	replacer := strings.NewReplacer(
		"forest", "",
		"cave", "",
		"rocks", "",
		"rock", "",
		"mountain", "",
		"landscape", "",
		"stone", "",
		"terrain", "",
		"amorphous", "",
		"blob", "",
		"sculpture", "",
		"minimalist sculpture", "",
	)
	out := replacer.Replace(in)
	out = strings.ReplaceAll(out, "  ", " ")
	out = strings.TrimSpace(strings.Trim(out, ","))
	return out
}

func extractCommercialIntent(in string) string {
	clean := strings.ToLower(strings.TrimSpace(in))
	if clean == "" {
		return ""
	}
	intent := make([]string, 0, 4)
	if strings.Contains(clean, "background") {
		intent = append(intent, "commercial technology background")
	}
	if strings.Contains(clean, "hero") {
		intent = append(intent, "website hero image ready")
	}
	if strings.Contains(clean, "banner") {
		intent = append(intent, "banner layout ready")
	}
	if strings.Contains(clean, "business") || strings.Contains(clean, "corporate") {
		intent = append(intent, "business innovation visual")
	}
	if len(intent) == 0 {
		return "commercial premium technology visual"
	}
	return strings.Join(intent, ", ")
}

func hasAnyToken(text string, tokens ...string) bool {
	for _, t := range tokens {
		if strings.Contains(text, t) {
			return true
		}
	}
	return false
}

func mysticalHeroMotifs() []string {
	return []string{
		"single floating glowing crystal core with internal cyan energy veins",
		"single luminous energy sphere with subtle halo rings",
		"single translucent digital lotus with refined glass petals",
		"single holographic cube with clean neon edges",
		"single mysterious tech energy artifact with premium glow",
	}
}

func randomNonEmpty(rng *rand.Rand, values []string) string {
	filtered := filterNonEmpty(values)
	if len(filtered) == 0 {
		return ""
	}
	return filtered[rng.Intn(len(filtered))]
}

func filterNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func normalizeSubNiche(value string, heroMode bool) string {
	value = strings.TrimSpace(value)
	if !heroMode || value == "" {
		return value
	}
	switch strings.ToLower(value) {
	case "glowing crystal core":
		return "single floating glowing crystal core with clean internal light veins"
	case "energy sphere":
		return "single premium floating energy sphere with soft halo rings"
	case "digital lotus":
		return "single translucent digital lotus with elegant luminous petals"
	case "holographic cube":
		return "single transparent holographic cube with precise neon edges"
	case "tech energy artifact":
		return "single futuristic tech artifact object with refined mystical glow"
	default:
		return "single hero object " + value
	}
}

func normalizeEnvironment(value string, heroMode bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !heroMode {
		return value
	}

	l := strings.ToLower(value)
	switch {
	case strings.Contains(l, "reflective"):
		return "minimal reflective premium surface"
	case strings.Contains(l, "energy field"):
		return "abstract energy field with controlled glow"
	case strings.Contains(l, "gradient"):
		return "dark gradient studio space"
	case strings.Contains(l, "forest"), strings.Contains(l, "nature"), strings.Contains(l, "cave"), strings.Contains(l, "rock"):
		return "subtle atmospheric mist only, no literal environment"
	default:
		return "dark minimal studio atmosphere"
	}
}

func normalizeBackground(value string, heroMode bool) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if !heroMode {
		return value
	}
	return "dark minimal premium gradient with subtle glow"
}

func isMysticalTechHeroNiche(niche, commercialUse string) bool {
	n := strings.ToLower(strings.TrimSpace(niche))
	c := strings.ToLower(strings.TrimSpace(commercialUse))
	return (strings.Contains(n, "mystical") || strings.Contains(n, "tech") || strings.Contains(n, "futuristic")) &&
		(strings.Contains(c, "adobe stock") || strings.Contains(c, "hero image") || strings.Contains(c, "background"))
}

func isFuturisticTechNiche(v string) bool {
	l := strings.ToLower(strings.TrimSpace(v))
	if l == "" {
		return false
	}
	return strings.Contains(l, "futuristic") ||
		strings.Contains(l, "technology") ||
		strings.Contains(l, "tech") ||
		strings.Contains(l, "abstract")
}

func ensureNoHumansToken(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	l := strings.ToLower(prompt)
	if strings.Contains(l, "no humans") || strings.Contains(l, "no human") || strings.Contains(l, "no people") {
		return prompt
	}
	if prompt == "" {
		return "no humans"
	}
	return prompt + ", no humans"
}
