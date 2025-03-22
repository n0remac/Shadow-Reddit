package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sashabaranov/go-openai"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // For local dev
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}
	client := openai.NewClient(apiKey)

	http.HandleFunc("/reddit", ServeNode(RedditHomePage()))

	http.HandleFunc("/reddit/new", ServeNode(RedditPromptPage()))

	http.HandleFunc("/reddit/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Error parsing form", http.StatusBadRequest)
			return
		}
		prompt := r.FormValue("prompt")
		subreddit := r.FormValue("subreddit")
		if prompt == "" {
			http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
			return
		}

		session := NewSession(prompt, subreddit)
		log.Printf("[INFO] Created session %s with prompt", session.ID)

		selected, err := SelectPersonas(client, prompt, Personas)
		if err != nil {
			log.Printf("[ERROR] selecting personas: %v", err)
			http.Error(w, "Failed to select personas", http.StatusInternalServerError)
			return
		}
		session.SelectedPersonas = selected

		log.Printf("[INFO] Created session %s with prompt", session.ID)

		http.Redirect(w, r, "/reddit/session?id="+session.ID, http.StatusSeeOther)
	})

	http.HandleFunc("/reddit/session", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing session ID", http.StatusBadRequest)
			return
		}
		session, ok := GetSession(id)
		if !ok {
			http.Error(w, "Invalid session ID", http.StatusNotFound)
			return
		}
		ServeNode(RedditSessionPage(session.Prompt, session.ID))(w, r)
	})

	http.HandleFunc("/reddit/ws", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "Missing session ID", http.StatusBadRequest)
			return
		}
		sess, ok := GetSession(id)
		if !ok {
			http.Error(w, "Invalid session", http.StatusNotFound)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			return
		}
		defer conn.Close()
		log.Printf("WebSocket connected for session %s", sess.ID)

		for _, persona := range sess.SelectedPersonas {
			text, err := GenerateResponseFromPersona(client, sess.Prompt, persona)
			if err != nil {
				log.Printf("[ERROR] generating response: %v", err)
				break
			}
			comment := SimulatedComment{
				Username: persona.Name,
				Flair:    persona.Style,
				Text:     text,
				Upvotes:  rand.Intn(500),
			}
			html := RenderSimulatedComment(comment).Render()
			msg := map[string]string{"type": "comment", "html": html}
			if err := conn.WriteJSON(msg); err != nil {
				log.Printf("WebSocket write error: %v", err)
				break
			}
			time.Sleep(2 * time.Second)
		}

		conn.WriteJSON(map[string]string{"type": "done"})
	})

	log.Println("[INFO] Listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func RedditHomePage() *Node {
	return DefaultLayout(
		Div(Class("container mx-auto p-8 text-center space-y-4"),
			H1(Class("text-3xl font-bold"), T("Welcome to the Reddit Simulation Tool")),
			P(Class("text-lg"),
				T("This app helps you reflect on complex emotional situations by simulating a Reddit thread with multiple perspectives."),
			),
			A(Href("/reddit/new"),
				Class("inline-block mt-4 text-blue-600 hover:underline"),
				T("Start a New Post"),
			),
		),
	)
}

func RenderSimulatedComment(c SimulatedComment) *Node {
	return Div(Class("bg-white p-4 rounded shadow mb-4"),
		Div(Class("flex items-center justify-between"),
			Span(Class("font-semibold text-blue-700"), Text(c.Username)),
			Span(Class("text-sm text-gray-500"), Text(c.Flair)),
		),
		P(Class("mt-2 text-gray-800"), Text(c.Text)),
		Div(Class("text-sm text-gray-500 mt-1"), Text(fmt.Sprintf("%d upvotes", c.Upvotes))),
	)
}

func RedditPromptPage() *Node {
	return DefaultLayout(
		Main(Class("max-w-2xl mx-auto p-8 space-y-6"),
			H1(Class("text-2xl font-bold"), T("ShadowReddit")),
			Form(Method("POST"), Action("/reddit/start"),
				Div(Class("mb-4"),
					Label(For("prompt"), Class("block font-medium mb-1"), T("Your Problem (Reddit-style post)")),
					TextArea(Id("prompt"), Name("prompt"), Class("w-full border rounded p-2"), Rows(6)),
				),
				Div(Class("mb-4"),
					Label(For("subreddit"), Class("block font-medium mb-1"), T("Simulated Subreddit")),
					Select(Name("subreddit"), Id("subreddit"), Class("w-full border rounded p-2"),
						Option(Value("aita"), T("r/AmITheAsshole")),
						Option(Value("relationships"), T("r/relationships")),
						Option(Value("legaladvice"), T("r/legaladvice")),
						Option(Value("askreddit"), T("r/AskReddit")),
					),
				),
				Button(Type("submit"), Class("bg-blue-600 text-white px-4 py-2 rounded"), T("Simulate Responses")),
			),
		),
	)
}

type Persona struct {
	Name           string
	Aliases        []string
	Style          string
	ResponseTraits string
}

type PersonaSelectionResponse struct {
	SelectedPersonas []Persona `json:"selected_personas"`
}

var Personas = []Persona{
	{
		Name:           "BrendaTheBoundary",
		Aliases:        []string{"EmpathyBrenda", "TherapistThrowaway"},
		Style:          "Therapist-adjacent, warm",
		ResponseTraits: "Advocates for emotional intelligence, boundaries, and healthy communication. Likely to say 'You deserve to be treated with respect.'",
	},
	{
		Name:           "SaltySage66",
		Aliases:        []string{"OldTimerSage", "SeenItAll66"},
		Style:          "Elder Redditor",
		ResponseTraits: "Sarcastic but wise. Uses personal anecdotes and a 'seen it all' tone. Might say 'Kid, you’re setting yourself up for pain.'",
	},
	{
		Name:           "JusticeJake",
		Aliases:        []string{"MoralityJake", "LawAndOrder123"},
		Style:          "Strong moral compass",
		ResponseTraits: "Comes down hard on ethical violations. Sees things in right/wrong binaries.",
	},
	{
		Name:           "NeutralNina",
		Aliases:        []string{"BalancedNina", "BothSidesBot"},
		Style:          "Fence-sitter",
		ResponseTraits: "Avoids judgments. Asks reflective questions to explore nuance.",
	},
	{
		Name:           "BluntBecca",
		Aliases:        []string{"RealTalkBecca", "CutToIt"},
		Style:          "No-nonsense realist",
		ResponseTraits: "Cuts to the chase. Doesn’t sugarcoat. Might say 'Grow up and move on.'",
	},
	{
		Name:           "EmpathyEli",
		Aliases:        []string{"SoftSoulEli", "FeelingFriend"},
		Style:          "Emotionally intelligent",
		ResponseTraits: "Gentle, understanding, encourages forgiveness and reflection.",
	},
	{
		Name:           "ChaosTom",
		Aliases:        []string{"WildCardTom", "MemePhilosopher"},
		Style:          "Trollish but insightful",
		ResponseTraits: "Contrarian takes. Wild analogies. Might say 'You’re all wrong, this is a simulation.'",
	},
	{
		Name:           "OverthinkOlivia",
		Aliases:        []string{"WallOfText", "ThinkTankOlivia"},
		Style:          "Analytical, verbose",
		ResponseTraits: "Breaks down every detail into over-analysis. Long responses.",
	},
	{
		Name:           "MomModeMarge",
		Aliases:        []string{"RedditMom", "ProtectiveMarge"},
		Style:          "Protective, older tone",
		ResponseTraits: "Motherly advice. Uses phrases like 'If you were my kid...'",
	},
	{
		Name:           "TinfoilTerry",
		Aliases:        []string{"ParanoiaPatrol", "PlotHoleDetective"},
		Style:          "Paranoid, skeptical",
		ResponseTraits: "Suspects hidden motives. Might say 'Sounds like this was a setup.'",
	},
	{
		Name:           "ZenZara",
		Aliases:        []string{"InnerPeace", "FloatAwayZara"},
		Style:          "Philosophical, abstract",
		ResponseTraits: "Encourages detachment. Stoic or Buddhist tone. 'Suffering is attachment.'",
	},
	{
		Name:           "DramaDan",
		Aliases:        []string{"SpillTheTea", "EmotionalDan"},
		Style:          "Overdramatizing, chaotic neutral",
		ResponseTraits: "Thrives on emotional drama. Overblown metaphors and chaos takes.",
	},
	{
		Name:           "TiredTina",
		Aliases:        []string{"SeenThisBefore", "JadedJane"},
		Style:          "Jaded by Reddit",
		ResponseTraits: "Dismissive tone. Often says 'These posts are always the same.'",
	},
	{
		Name:           "HeartfeltHenry",
		Aliases:        []string{"HopefulHenry", "KindnessKarma"},
		Style:          "Kind, optimistic",
		ResponseTraits: "Believes people can grow. Encourages kindness and reconciliation.",
	},
	{
		Name:           "SystemSam",
		Aliases:        []string{"MacroLens", "SociologyNerd"},
		Style:          "Systems thinker",
		ResponseTraits: "Sees structural patterns and power dynamics behind personal events.",
	},
	{
		Name:           "LonelyLarry",
		Aliases:        []string{"SoloSoul", "SadBoyMode"},
		Style:          "Vulnerable, projecting",
		ResponseTraits: "Often turns things inward. Comments can feel somber or self-focused.",
	},
	{
		Name:           "TrollPatrol",
		Aliases:        []string{"RulesBot123", "AITAJudgeDredd"},
		Style:          "Technical, pedantic",
		ResponseTraits: "Strict on format, tone, and logic. Often asks for INFO or clarifies OP’s assumptions.",
	},
	{
		Name:           "YesQueenYasmin",
		Aliases:        []string{"HypeTrainYas", "BoldAdviceBabe"},
		Style:          "Supportive hype friend",
		ResponseTraits: "Big energy. Will yell 'DUMP HIM 🔥' and mean it.",
	},
	{
		Name:           "LibrarianLee",
		Aliases:        []string{"CiteYourSources", "ReferenceRobot"},
		Style:          "Academic, precise",
		ResponseTraits: "Links studies, sources, or Reddit posts. Rarely comments without references.",
	},
	{
		Name:           "PastPainPaul",
		Aliases:        []string{"TraumaTalkPaul", "HardLessons"},
		Style:          "Trauma-informed",
		ResponseTraits: "Brings heavy emotional experience. May say 'This reminds me of what I went through…'",
	},
}

func SelectPersonas(client *openai.Client, userPrompt string, allPersonas []Persona) ([]Persona, error) {
	personasJSON, err := json.Marshal(allPersonas)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal personas: %w", err)
	}

	systemPrompt := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: "You are selecting a diverse and relevant set of personas to simulate Reddit-style responses to the user's post. Your goal is to include a mix of perspectives (e.g., supportive, skeptical, moral, humorous, empathetic). The number of personas should be between 5 and 8. Avoid redundancy.",
	}

	userMessage := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: fmt.Sprintf("Post: %s\nAvailable Personas: %s", userPrompt, string(personasJSON)),
	}

	fn := openai.FunctionDefinition{
		Name:        "select_personas",
		Description: "Choose a diverse set of personas to respond to a Reddit-style thread",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selected_personas": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"name":            map[string]any{"type": "string"},
							"aliases":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"style":           map[string]any{"type": "string"},
							"response_traits": map[string]any{"type": "string"},
						},
						"required": []string{"name", "aliases", "style", "response_traits"},
					},
				},
			},
			"required": []string{"selected_personas"},
		},
	}

	chatRequest := openai.ChatCompletionRequest{
		Model: "gpt-4-0613",
		Messages: []openai.ChatCompletionMessage{
			systemPrompt,
			userMessage,
		},
		Functions:    []openai.FunctionDefinition{fn},
		FunctionCall: openai.FunctionCall{Name: "select_personas"},
	}

	chatResp, err := client.CreateChatCompletion(context.Background(), chatRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get response from OpenAI: %w", err)
	}

	choice := chatResp.Choices[0]
	if choice.Message.FunctionCall == nil {
		return nil, fmt.Errorf("no function call in OpenAI response")
	}

	var parsed PersonaSelectionResponse
	err = json.Unmarshal([]byte(choice.Message.FunctionCall.Arguments), &parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal function response: %w", err)
	}

	return parsed.SelectedPersonas, nil
}

func GenerateResponseFromPersona(client *openai.Client, prompt string, persona Persona) (string, error) {
	systemMsg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf("You are roleplaying as a Reddit commenter named %s. You speak in a style described as: '%s'. Your responses are characterized by: '%s'. Write a single Reddit comment as this persona.", persona.Name, persona.Style, persona.ResponseTraits),
	}

	userMsg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: fmt.Sprintf("Here is the Reddit post: %s", prompt),
	}

	resp, err := client.CreateChatCompletion(context.Background(), openai.ChatCompletionRequest{
		Model:    openai.GPT4,
		Messages: []openai.ChatCompletionMessage{systemMsg, userMsg},
	})
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

// Each user gets a RedditSession
type RedditSession struct {
	ID               string
	Prompt           string
	Subreddit        string
	SelectedPersonas []Persona
	PostContent      string
	Responses        []SimulatedComment
	Done             bool
	Error            error
}

// Comment-style response from a Reddit simulation
type SimulatedComment struct {
	Username string
	Flair    string
	Text     string
	Upvotes  int
	Replies  []SimulatedComment
}

// Session store
var (
	sessions      = make(map[string]*RedditSession)
	sessionsMutex sync.Mutex
)

func RedditSessionPage(prompt string, sessionID string) *Node {
	return DefaultLayout(
		Div(Class("max-w-2xl mx-auto p-6 space-y-6"),
			H1(Class("text-2xl font-bold"), T("Your Reddit Simulation")),
			Div(Class("bg-gray-100 p-4 rounded"),
				H2(Class("font-semibold text-lg"), T("Your Post")),
				P(Class("mt-2 whitespace-pre-wrap text-gray-800"), Text(prompt)),
			),
			Div(Id("responseArea"),
				P(Class("text-gray-500 italic"), T("Generating simulated responses...")),
				Div(Class("mt-2"),
					Progress(Class("progress progress-primary w-full"), Max("100")),
				),
			),
			Script(Raw(fmt.Sprintf(`
				const evtSource = new EventSource("/reddit/events?id=%s");
				evtSource.onmessage = function(event) {
					document.getElementById("responseArea").innerHTML = event.data;
				};
			`, sessionID))),
		),
		Script(Raw(fmt.Sprintf(`
			let ws = new WebSocket("ws://" + window.location.host + "/reddit/ws?id=%s");
			let responseArea = document.getElementById("responseArea");

			ws.onmessage = function(event) {
				let data = JSON.parse(event.data);
				if (data.type === "comment") {
					let div = document.createElement("div");
					div.innerHTML = data.html;
					responseArea.appendChild(div);
				} else if (data.type === "done") {
					let p = document.createElement("p");
					p.innerText = "Simulation complete.";
					responseArea.appendChild(p);
					ws.close();
				}
			};
		`, sessionID))),
	)
}

// Create a new session and store it
func NewSession(prompt, subreddit string) *RedditSession {
	id := randomID()
	s := &RedditSession{
		ID:        id,
		Prompt:    prompt,
		Subreddit: subreddit,
	}
	sessionsMutex.Lock()
	sessions[id] = s
	sessionsMutex.Unlock()
	return s
}

// Retrieve session by ID
func GetSession(id string) (*RedditSession, bool) {
	sessionsMutex.Lock()
	defer sessionsMutex.Unlock()
	s, ok := sessions[id]
	return s, ok
}

// Simple random ID generator (12-char)
func randomID() string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}
