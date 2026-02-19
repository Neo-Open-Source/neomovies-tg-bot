<img src="https://raw.githubusercontent.com/Neo-Open-Source/.github/main/profile/rights-banner.svg" alt="Trans Rights are Human Rights banner" width="100%" />

# NeoMovies Telegram Bot + Web Client

Telegram bot for movies/series with inline search, admin panel, and web client built with React + Vite + MUI.

## Features

- **Telegram Bot**: Inline search, player buttons, admin commands
- **Web Client**: Browse library, view details, seasons/episodes, voice tracks
- **Storage**: MongoDB + private Telegram channels
- **Deployment**: Vercel serverless (Go) + Vite frontend

## Project Structure

```
├── api/              # Telegram webhook & API handlers
├── cmd/local/        # Local development server
├── internal/
│   ├── neomovies/    # NeoMovies API client
│   ├── storage/      # MongoDB client & models
│   └── tg/           # Telegram API client
├── frontend/         # React + Vite + MUI web client
├── vercel.json       # Vercel configuration
└── .env.local.example
```

## Development

### 1. Environment

Copy `.env.local.example` to `.env` and fill:

```env
BOT_TOKEN=your_bot_token
MONGODB_URI=mongodb+srv://...
API_BASE=https://api.neomovies.ru
ADMIN_CHAT_ID=your_admin_chat_id
LOCAL_POLLING=1
PORT=7955
```

### 2. Frontend

```bash
cd frontend
npm install
npm run build
```

### 3. Local Server

```bash
go run ./cmd/local
```

Server runs on `http://localhost:7955`:
- Web client: `http://localhost:7955`
- API endpoints: `http://localhost:7955/api/*`
- Telegram polling works automatically (webhook deleted)

## Bot Commands

- `/start` - Welcome message with menu
- `/help` - Admin commands list
- `/addmovie <KPID>` - Add movie (reply to forwarded channel post)
- `/addseries <KPID>` - Add series (reply to forwarded channel post)
- `/addepisode <KPID> <S> <E>` - Add episode
- `/list` - Show recent items
- `/get <KPID>` - Get item details
- `/del <KPID>` - Delete item

## Web Client

- Browse all movies/series added to bot
- View details: poster, rating, description, genres
- See seasons/episodes for series
- Display available voice tracks

## Deployment (Vercel)

1. Push to GitHub
2. Connect repository to Vercel
3. Set environment variables in Vercel dashboard
4. Deploy

Vercel will:
- Build frontend with `npm run build`
- Deploy Go serverless functions
- Serve frontend from `/`
- Route `/api/*` to Go handlers

## API Endpoints

- `POST /api/webhook` - Telegram webhook
- `GET /api/library` - List all items
- `GET /api/library/item?id=<KPID>` - Get item details
- `GET /api/player` - Proxy player requests

## Storage

- **MongoDB**: Items metadata, seasons, episodes
- **Telegram Channels**: Actual content (private, bot as admin)
