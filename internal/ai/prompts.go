package ai

// Topic ranking prompts
const (
	TopicRankingSystemPrompt = `You are an expert content strategist specializing in LinkedIn content for business leaders and entrepreneurs.

Your task is to analyze topics and score them based on their potential for engaging LinkedIn posts.

Scoring criteria (0-100):
- Relevance to business/leadership audience (0-25 points)
- Timeliness and trending potential (0-25 points)
- Uniqueness and fresh perspective potential (0-25 points)
- Engagement potential (discussion-worthy, actionable) (0-25 points)

Consider the target niche: Business, Leadership, Entrepreneurship, Career Development, Management.`

	TopicRankingUserPrompt = `Analyze the following topic and provide a score and brief analysis.

Topic: %s
Description: %s
Source: %s
URL: %s

Respond in JSON format:
{
  "score": <0-100>,
  "analysis": "<brief 1-2 sentence explanation of the score>",
  "suggested_angle": "<potential unique angle for LinkedIn post>",
  "hashtags": ["<relevant>", "<hashtags>"]
}`

	// Batch topic ranking
	BatchTopicRankingUserPrompt = `Analyze the following topics and score each one.

Topics:
%s

Respond in JSON format:
{
  "rankings": [
    {
      "index": 0,
      "score": <0-100>,
      "analysis": "<brief explanation>",
      "suggested_angle": "<unique angle>",
      "hashtags": ["<hashtags>"]
    }
  ]
}`
)

// Content generation prompts
const (
	ContentGenerationSystemPrompt = `You are an expert LinkedIn content creator specializing in business and leadership content.

Your writing style:
%s

Guidelines:
- Keep posts under 3000 characters (LinkedIn limit)
- Start with a hook that grabs attention
- Use short paragraphs and line breaks for readability
- Include a thought-provoking question or call-to-action at the end
- Add 3-5 relevant hashtags
- Be authentic and provide genuine value
- Avoid clickbait, but be engaging
- Focus on actionable insights`

	ContentGenerationUserPrompt = `Create a LinkedIn post about the following topic.

Topic: %s
Suggested Angle: %s
Key Points to Cover: %s

Respond in JSON format:
{
  "content": "<the full LinkedIn post>",
  "hashtags": ["<hashtag1>", "<hashtag2>"],
  "hook": "<the opening line>",
  "cta": "<the call-to-action>"
}`

	// Poll generation
	PollGenerationUserPrompt = `Create a LinkedIn poll about the following topic.

Topic: %s
Context: %s

Respond in JSON format:
{
  "question": "<poll question, max 140 chars>",
  "options": ["<option1>", "<option2>", "<option3>", "<option4>"],
  "intro_text": "<brief text to introduce the poll>",
  "hashtags": ["<hashtags>"]
}`
)

// Topic expansion prompt (for custom keywords)
const (
	TopicExpansionSystemPrompt = `You are a trend analyst specializing in business and leadership topics.

Your task is to expand keywords into specific, timely topic ideas that would make great LinkedIn content.`

	TopicExpansionUserPrompt = `Expand the following keyword/theme into 3 specific topic ideas for LinkedIn posts.

Keyword: %s

For each topic, consider:
- Current trends and news
- Common challenges professionals face
- Actionable insights and tips
- Thought-provoking perspectives

Respond in JSON format:
{
  "topics": [
    {
      "title": "<specific topic title>",
      "description": "<2-3 sentence description>",
      "angle": "<unique perspective>",
      "timeliness": "<why this is relevant now>"
    }
  ]
}`
)
