# ShadowReddit

ShadowReddit is an interactive tool that simulates a Reddit thread using AI-generated personas. It helps users reflect on personal or emotional situations by presenting a range of perspectives — from empathetic to sarcastic, moralistic to chaotic — mirroring the diversity of real Reddit replies.

Built entirely in Go, it uses OpenAI’s GPT-4 to select personas and generate responses, all rendered in real time through WebSockets.

## 🛠 Setup

1. Set your OpenAI API key:

```bash
export OPENAI_API_KEY=your-key-here
```

2. Run the server:
```
go run .
```

3. Open your browser and visit:
    
http://localhost:8080/reddit

Start a new post and watch the simulation unfold in real time.
