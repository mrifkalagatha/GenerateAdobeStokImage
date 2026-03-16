# AI Generator Service (Gin + Golang)

REST API service untuk pipeline AI image generation dengan integrasi Leonardo AI, async video worker, dan ZIP export.

## Requirements

- Go `1.23+`
- Leonardo API Key (`LEONARDO_API_KEY`)

## Project Structure

```text
ai-generator/
  main.go
  config/
  controllers/
  services/
  workers/
  utils/
  routes/
  docs/
  storage/
```

## Environment Variables

```env
PORT=8080
STORAGE_PATH=storage

LEONARDO_API_KEY=your_api_key_here
LEONARDO_API_KEYS=key_1,key_2,key_3
LEONARDO_BASE_URL=https://cloud.leonardo.ai/api/rest/v1
LEONARDO_GENERATE_ENDPOINT=/generations
LEONARDO_UPSCALE_ENDPOINT=/variations/upscale
LEONARDO_VIDEO_ENDPOINT=/generations-text-to-video
LEONARDO_GENERATION_GET_PATH=/generations/{id}
LEONARDO_MODEL_ID=2067ae52-33fd-4a82-bb92-c2c55e7d2786
LEONARDO_STYLE_UUID=111dc692-d470-4eec-b791-3475abac4c46
LEONARDO_DEFAULT_ALCHEMY=false
LEONARDO_REQUIRE_MODEL_ID=true

HTTP_TIMEOUT_SECONDS=60
RETRY_COUNT=3
RETRY_DELAY_MS=1200
POLL_ATTEMPTS=20
POLL_INTERVAL_MS=2000

MAX_IMAGE_WORKERS=5
MAX_ENHANCE_WORKERS=5
MAX_VIDEO_WORKERS=2
VIDEO_QUEUE_SIZE=200

VARIANT_MIN=3
VARIANT_MAX=5
MAX_IMAGES_PER_PROMPT=10
MAX_TOTAL_IMAGES=500
PROMPT_CATALOG_PATH=data/prompt_catalog.json
SEASONAL_EVENT_PATH=data/seasonal_events.json

# Optional untuk local testing tanpa call API Leonardo
LEONARDO_MOCK=false

OPENAI_API_KEY=
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_MODEL=gpt-4o-mini
NICHE_PROMPT_COUNT=8
LEONARDO_MAX_NUM_IMAGES=2
STOCK_TARGET_WIDTH=3840
STOCK_TARGET_HEIGHT=2160
STOCK_MIN_PIXELS=4000000
```

## Install & Run

```bash
go mod tidy
go run .
```

Service jalan di:

`http://localhost:8080`

## API Endpoints

- `POST /api/v1/generate`
- `GET /api/generate/stream` (SSE)
- `GET /download/{job_id}`
- `GET /health`

## Example Request

`POST /api/v1/generate`

```json
{
  "prompt": "a glowing horizon made of tiny light nodes forming an abstract data landscape, deep blue and violet tones, minimal futuristic technology background, cinematic lighting, ultra detailed, no humans",
  "file": "image",
  "auto_metadata": true,
  "enhance": true,
  "generate": 4,
  "debug": false,
  "auto_prompt": false,
  "strict_single_object": false,`n  "variation_strength": "medium",`n  "aspect_ratio": "16:9",
  "model_id": "2067ae52-33fd-4a82-bb92-c2c55e7d2786",
  "style_uuid": "111dc692-d470-4eec-b791-3475abac4c46",
  "seed": 12345
}
```

`auto_prompt=false` = prompt manual passthrough (tidak di-hardening) agar hasil lebih mirip output Leonardo Web untuk prompt yang sama.
`file` menentukan tipe output: `image`, `video`, atau `vector`.
`auto_metadata=true` akan membuat `adobe_stock_metadata.csv` otomatis untuk semua hasil menggunakan ChatGPT (dengan fallback lokal jika OpenAI gagal).
`generate` adalah total target file sesuai tipe yang dipilih. Sistem membagi batch otomatis sesuai limit Leonardo.
`model_id` wajib jika `LEONARDO_REQUIRE_MODEL_ID=true` (default), supaya tidak fallback ke model default seperti Stable Diffusion 1.5.`n`variation_strength` opsional (`low|medium|high`) untuk mengontrol seberapa kuat perbedaan antar hasil.

Debug low-cost mode:

```json
{
  "niche": "abstract futuristic technology background",
  "debug": true
}
```

When `debug=true`, service forces:
- `generate=1`
- only first prompt/variant
- `enhance=true`
- keep `file` mode as requested

Auto prompt mode by niche:

```json
{
  "niche": "mystical futuristic tech object",
  "sub_niche": [
    "glowing crystal core",
    "energy sphere",
    "digital lotus"
  ],
  "environment": [
    "dark gradient space",
    "reflective minimal surface"
  ],
  "color_palette": ["cyan", "blue", "violet"],
  "lighting": "cinematic volumetric glow",
  "detail_level": "ultra",
  "commercial_use": "adobe stock background and hero image",
  "aspect_ratio": "16:9",
  "file": "image",
  "enhance": true,
  "generate": 4
}
```

If `niche` is provided, prompts are always regenerated via ChatGPT using:
- your niche as primary constraint
- optional `sub_niche` object families
- weekly high-demand search intent
- optional style/composition/background/color/environment/lighting/commercial hints
- seasonal context from nearest international days / monthly commercial themes

## Prompt Intelligence Flow

Saat request menggunakan `niche`, sistem sekarang memakai alur prompt yang lebih agresif untuk mengejar kualitas Adobe Stock:

1. `Niche analysis`
- membaca `niche`, `style`, `composition`, `background`, `color_palette`, `complexity`

2. `Seasonal intelligence`
- membaca file lokal:
  - `data/seasonal_events.json`
- mengecek apakah bulan ini dekat dengan:
  - hari internasional
  - seasonal campaign
  - tema komersial bulanan
- contoh:
  - `World Water Day`
  - `International Day of Happiness`
  - `Earth Day`
  - `World Health Day`

3. `AI prompt generation`
- OpenAI diminta membuat prompt yang:
  - tetap sesuai niche utama
  - mengikuti pencarian stock mingguan yang sedang tinggi
  - menyesuaikan seasonal event hanya jika masih relevan dengan niche
  - tetap aman untuk Adobe Stock:
    - no humans
    - no text
    - no logo
    - no watermark
    - commercial-safe

4. `Market scoring`
- setiap prompt hasil AI diberi skor sebelum dikirim ke Leonardo
- scoring menilai:
  - niche match
  - commercial campaign relevance
  - stock-friendly composition
  - copy space / negative space usability
  - premium visual quality
  - seasonal demand fit
  - brand safety / stock compliance

5. `Prompt ranking`
- prompt dengan skor tertinggi diprioritaskan
- top scored prompts dipakai untuk batch generation Leonardo

6. `Fallback local catalog`
- jika OpenAI gagal / quota limit:
  - fallback ke `data/prompt_catalog.json`
- fallback prompt juga tetap diberi seasonal hint dari `data/seasonal_events.json`

## Seasonal Event File

File:
- `data/seasonal_events.json`

File ini bisa kamu edit sendiri untuk menambah:
- nama event
- tanggal event
- keyword niche yang relevan
- prompt cues
- stock angles

Contoh konsep yang dihasilkan sistem:
- niche utama: `abstract futuristic technology background`
- seasonal dekat `World Water Day`
- hasil prompt bisa bergeser ke:
  - clean eco technology fusion
  - liquid clarity concept
  - sustainable future background
  - water innovation symbolism

Dengan begitu prompt tidak hanya indah, tapi juga lebih dekat ke kebutuhan campaign, landing page, presentation background, dan stock commercial usage.

If ChatGPT quota is exceeded or request fails, service automatically falls back to local prompt catalog:
- `data/prompt_catalog.json`

Leonardo multi-key failover:
- You can set multiple keys in `LEONARDO_API_KEYS` (comma-separated).
- If active key hits limit/quota, service automatically retries with next key.
- `LEONARDO_API_KEY` is still supported and can be used as primary key.

## Market Scoring Log

Saat generate berjalan, terminal backend / FE akan menampilkan log ranking prompt seperti:

```text
market score=82 prompt=...
market score=79 prompt=...
market score=74 prompt=...
```

Artinya sistem sedang memilih prompt yang paling potensial untuk niche dan momentum pasar saat ini sebelum dikirim ke Leonardo.

## Example Success Response

```json
{
  "status": "success",
  "message": "generation completed",
  "data": {
    "job_id": "job_83492311aa22",
    "file_type": "image",
    "total_prompt": 2,
    "total_image": 8,
    "total_file": 8,
    "zip_file": "/download/job_83492311aa22"
  }
}
```

## Download Result ZIP

Setelah dapat `job_id`, download:

`GET /download/{job_id}`

Contoh:

`GET /download/job_83492311aa22`

## Notes

- Video diproses async (worker pool), tidak memblok response utama.
- Video sekarang divalidasi sebagai MP4 binary sebelum disimpan (mencegah file `.mp4` corrupt berisi HTML/JSON).
- Mode `file=vector` sekarang menghasilkan file `.svg` asli (hasil auto-vectorize dari output AI), bukan JPG/PNG.
- File hasil disimpan di:
  - `storage/YYYY/MM/DD/{job_id}/`
- Dokumentasi endpoint detail ada di:
  - `docs/API.md`
- Seasonal event source editable di:
  - `data/seasonal_events.json`
- Local fallback prompt source editable di:
  - `data/prompt_catalog.json`
