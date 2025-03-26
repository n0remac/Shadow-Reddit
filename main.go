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

// ---------- DATA STRUCTURES ----------

// A single stance: e.g., "supportive", "strong_agreement", with a short summary
type Stance struct {
	Type    string `json:"type"`
	SubType string `json:"subtype"`
	Summary string `json:"summary"`
}

// The function-call response structure for stance selection
type StanceSelectionResponse struct {
	Stances []Stance `json:"stances"`
}

// Each user gets a RedditSession
type RedditSession struct {
	ID              string
	Prompt          string
	Subreddit       string
	SelectedStances []Stance // The stances chosen by GPT
	Responses       []SimulatedComment
	Done            bool
	Error           error
}

// Comment-style response from a Reddit simulation
type SimulatedComment struct {
	Username string
	Flair    string
	Text     string
	Replies  []SimulatedComment
}

// Session store (in-memory for now)
var (
	sessions      = make(map[string]*RedditSession)
	sessionsMutex sync.Mutex
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // For local dev
}

// ---------- MAIN + ROUTES ----------

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY not set")
	}
	client := openai.NewClient(apiKey)

	http.HandleFunc("/", ServeNode(RedditHomePage()))
	http.HandleFunc("/new", ServeNode(RedditPromptPage()))

	http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
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

		// Create and store the session
		session := NewSession(prompt, subreddit)
		log.Printf("[INFO] Created session %s", session.ID)

		// Kick off AI work in background goroutine
		go func(sess *RedditSession) {
			var wg sync.WaitGroup

			// 1) Get stances from GPT
			selectedStances, err := generateStances(client, subreddit, prompt)
			if err != nil {
				log.Printf("[ERROR] generating stances: %v", err)
				sess.Error = err
				sess.Done = true
				return
			}

			// 2) Store stances in the session
			sessionsMutex.Lock()
			sess.SelectedStances = selectedStances
			sessionsMutex.Unlock()

			// 3) For each stance, generate a single top-level comment
			for _, stance := range selectedStances {
				text, err := GenerateResponseFromStance(client, prompt, stance)
				if err != nil {
					log.Printf("[ERROR] generating response: %v", err)
					sess.Error = err
					break
				}

				// Build the top-level comment
				comment := SimulatedComment{
					Username: fmt.Sprintf("%s_%s", stance.Type, stance.SubType),
					Flair:    stance.Type,
					Text:     text,
				}

				// Append to session and get its index
				sessionsMutex.Lock()
				idx := len(sess.Responses)
				sess.Responses = append(sess.Responses, comment)
				sessionsMutex.Unlock()

				// Spawn a goroutine to generate a reply for THIS top-level comment
				wg.Add(1)
				go func(parentIndex int, parentText string) {
					defer wg.Done()

					replyText, err := GenerateReplyToComment(client, sess.Prompt, parentText)
					if err != nil {
						log.Printf("[ERROR] generating reply: %v", err)
						// We'll just log the error. We won't stop the entire session.
						return
					}

					child := SimulatedComment{
						Username: randomReplyUsername(),
						Flair:    "reply",
						Text:     replyText,
					}

					sessionsMutex.Lock()
					sess.Responses[parentIndex].Replies = append(sess.Responses[parentIndex].Replies, child)
					sessionsMutex.Unlock()
				}(idx, text)
			}

			// 4) Once ALL replies are done, mark the session done
			go func() {
				wg.Wait()
				sess.Done = true
			}()
		}(session)

		http.Redirect(w, r, "/session?id="+session.ID, http.StatusSeeOther)
	})

	http.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
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

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
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

		log.Printf("WebSocket connected for session %s", id)

		lastSentTopLevel := 0
		replyCounts := make([]int, 0)

		// In your loop setup, you might do:
		sessionsMutex.Lock()
		replyCounts = make([]int, len(sess.Responses))
		sessionsMutex.Unlock()

		for {
			sessionsMutex.Lock()
			done := sess.Done

			// 1) Check if any new top-level comments arrived
			for lastSentTopLevel < len(sess.Responses) {
				comment := sess.Responses[lastSentTopLevel]
				html := RenderCommentRecursive(comment, 0).Render()
				fmt.Println("Rendering comment:", html)
				conn.WriteJSON(map[string]string{
					"type":        "comment",
					"parentIndex": fmt.Sprintf("%d", lastSentTopLevel),
					"html":        html,
				})
				lastSentTopLevel++
				replyCounts = append(replyCounts, len(comment.Replies))
			}

			// 2) Check each existing comment for new replies
			for i, comment := range sess.Responses {
				newReplyCount := len(comment.Replies)
				if newReplyCount > replyCounts[i] {
					// We have new replies
					for r := replyCounts[i]; r < newReplyCount; r++ {
						singleReply := comment.Replies[r]
						replyHTML := RenderCommentRecursive(singleReply, 1).Render()
						// We'll also send info about which parent index or comment ID to attach to
						conn.WriteJSON(map[string]string{
							"type":        "reply",
							"parentIndex": fmt.Sprintf("%d", i),
							"html":        replyHTML,
						})
					}
					replyCounts[i] = newReplyCount
				}
			}

			sessionsMutex.Unlock()

			if done {
				conn.WriteJSON(map[string]string{"type": "done"})
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	})

	log.Println("[INFO] Listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// ---------- LAYOUT / TEMPLATES ----------

// Home page
func RedditHomePage() *Node {
	return DefaultLayout(
		Div(Class("container mx-auto p-8 text-center space-y-4"),
			H1(Class("text-3xl font-bold"), T("Welcome to the Reddit Simulation Tool")),
			P(Class("text-lg"),
				T("This app helps you reflect on complex emotional situations by simulating a Reddit thread with multiple perspectives."),
			),
			A(Href("/new"),
				Class("inline-block mt-4 text-blue-600 hover:underline"),
				T("Start a New Post"),
			),
		),
		Footer(
			Class("text-center text-sm text-gray-500"),
			T("ShadowReddit is not affiliated with Reddit in anyway."),
		),
	)
}

// Page for user input
func RedditPromptPage() *Node {
	return DefaultLayout(
		Main(Class("max-w-2xl mx-auto p-8 space-y-6"),
			H1(Class("text-2xl font-bold"), T("ShadowReddit")),
			Form(Method("POST"), Action("/start"),
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

// RenderCommentRecursive renders a single comment, then any child replies.
// 'indentLevel' tells us how far to indent for nested replies.
func RenderCommentRecursive(c SimulatedComment, indentLevel int) *Node {
	indentClass := fmt.Sprintf("ml-%d", indentLevel*6) // or any indentation you like

	// Render this comment
	mainComment := Div(Class(fmt.Sprintf("bg-white p-4 rounded shadow mb-4 %s", indentClass)),
		Div(Class("flex items-center justify-between"),
			Span(Class("font-semibold text-blue-700"), Text(c.Username)),
			Span(Class("text-sm text-gray-500"), Text(c.Flair)),
		),
		P(Class("mt-2 text-gray-800"), Text(c.Text)),
	)

	// If no replies, just return
	if len(c.Replies) == 0 {
		return mainComment
	}

	// Container for nested replies
	replyNodes := []*Node{mainComment}
	for _, child := range c.Replies {
		// Recursively render each child, incrementing indent
		childNode := RenderCommentRecursive(child, indentLevel+1)
		replyNodes = append(replyNodes, childNode)
	}

	// Combine this comment + all children
	return Div(replyNodes...)
}

// Page that displays the simulated responses
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
	let ws = new WebSocket("ws://" + window.location.host + "/ws?id=%s");
	let responseArea = document.getElementById("responseArea");

	ws.onmessage = function(event) {
		let data = JSON.parse(event.data);

		if (data.type === "comment") {
			// Create a container for this top-level comment
			let parentDiv = document.createElement("div");
			parentDiv.setAttribute("id", "comment-" + data.parentIndex);
			parentDiv.innerHTML = data.html;
			responseArea.appendChild(parentDiv);

		} else if (data.type === "reply") {
			// Append a reply to an existing comment's container
			let parentDiv = document.getElementById("comment-" + data.parentIndex);
			if (!parentDiv) {
				console.warn("No parent container found for index", data.parentIndex);
				return;
			}
			let replyDiv = document.createElement("div");
			replyDiv.innerHTML = data.html;
			parentDiv.appendChild(replyDiv);

		} else if (data.type === "done") {
			// Signal that simulation is complete
			let p = document.createElement("p");
			p.innerText = "Simulation complete.";
			responseArea.appendChild(p);
			ws.close();
		}
	};
`, sessionID))),
		),
	)
}

// ---------- HELPER FUNCTIONS ----------

// Creates a new session
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

// Retrieves a session by ID
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

func randomReplyUsername() string {
	names := []string{
		"ReplyMaster",
		"CuriousCat",
		"HonestAbe",
		"DebateKing",
		"FriendlyNeighbor",
		"JustSaying",
		"RandomUser",
		"WittyRemark",
		"SkepticalSam",
		"AgreeableAlex",
	}
	return names[rand.Intn(len(names))]
}

// ---------- AI FUNCTIONS ----------

// generateStances picks 5-8 stances from AllStances using GPT's function-calling
func generateStances(client *openai.Client, thread string, post string) ([]Stance, error) {
	// Create a JSON-safe string version of AllStances to pass to GPT
	allStancesJSON, err := json.Marshal(AllStances)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal AllStances: %w", err)
	}

	systemPrompt := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `You are helping choose a set of stances for a Reddit thread.
Select 5 to 8 stances from a given list of predefined options. Choose perspectives that would likely be given. Do not invent new stances.
Use only stances from the provided list. It is ok if stances are repeated.`,
	}

	userMessage := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser,
		Content: fmt.Sprintf(`Reddit Thread Title: %s
Post Content: %s

Here is the full list of allowed stances (with type, subtype, and summary):
%s`, thread, post, string(allStancesJSON)),
	}

	fn := openai.FunctionDefinition{
		Name:        "select_stances",
		Description: "Select 5 to 8 stances from a list of predefined options",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"stances": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"type":    map[string]any{"type": "string"},
							"subtype": map[string]any{"type": "string"},
							"summary": map[string]any{"type": "string"},
						},
						"required": []string{"type", "subtype", "summary"},
					},
				},
			},
			"required": []string{"stances"},
		},
	}

	chatRequest := openai.ChatCompletionRequest{
		Model: "gpt-4-0613",
		Messages: []openai.ChatCompletionMessage{
			systemPrompt,
			userMessage,
		},
		Functions:    []openai.FunctionDefinition{fn},
		FunctionCall: openai.FunctionCall{Name: "select_stances"},
	}

	chatResp, err := client.CreateChatCompletion(context.Background(), chatRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get response from OpenAI: %w", err)
	}

	choice := chatResp.Choices[0]
	if choice.Message.FunctionCall == nil {
		return nil, fmt.Errorf("no function call in OpenAI response")
	}

	var parsed StanceSelectionResponse
	err = json.Unmarshal([]byte(choice.Message.FunctionCall.Arguments), &parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal function response: %w", err)
	}

	return parsed.Stances, nil
}

// GenerateResponseFromStance creates a single Reddit comment from a stance + user prompt
func GenerateResponseFromStance(client *openai.Client, prompt string, stance Stance) (string, error) {
	systemMsg := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(
			`You are a Reddit commenter who holds the following stance:
Type: %s
SubType: %s
Summary: %s

Write a single Reddit comment responding to the user's post from this perspective.
Your response should sound like a typical Reddit user with that viewpoint.
`,
			stance.Type, stance.SubType, stance.Summary,
		),
	}

	userMsg := openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: fmt.Sprintf("Here is the Reddit post:\n%s", prompt),
	}

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:    openai.GPT4,
			Messages: []openai.ChatCompletionMessage{systemMsg, userMsg},
		},
	)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

func GenerateReplyToComment(client *openai.Client, originalPost, parentComment string) (string, error) {
	fmt.Println("Generating reply to comment")
	systemMsg := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `You are simulating a reply in a Reddit thread. 
        You have the original post and a parent comment. 
        Write a single reply as if you are another Reddit user. 
        Keep it natural and typical of Reddit discussions.`,
	}

	userMsg := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser,
		Content: fmt.Sprintf(`ORIGINAL POST:
%s

PARENT COMMENT:
%s

Please write a single short reply to the parent comment.`, originalPost, parentComment),
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
