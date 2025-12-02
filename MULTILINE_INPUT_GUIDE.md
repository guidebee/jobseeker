# Multi-line Input Guide

## Overview

The `jobseeker checkjd` command now supports **multi-line text input** for both initial context and refinement feedback. You can copy/paste text from any source!

## When You Can Use Multi-line Input

### 1. Initial Context (Cover Letter Generation)
When prompted for additional context about yourself:
```
You can provide additional context for the cover letter (optional).
For example: specific projects, achievements, or why you're interested.
You can paste multiple lines of text.

Your context (or press Enter to skip):
(Type END on a new line when done, or paste text and press Ctrl+D/Ctrl+Z)
>
```

### 2. Refinement Feedback
When refining a generated cover letter:
```
What would you like to change?
Examples: 'Make it more concise', 'Add more technical details',
          'Emphasize leadership experience', 'More enthusiastic tone'
You can paste multiple lines of detailed feedback.

Your feedback:
(Type END on a new line when done, or paste text and press Ctrl+D/Ctrl+Z)
>
```

## How to Use Multi-line Input

### Method 1: Type and End with "END"
```
> This is my first line
> This is my second line
> This is my third line
> END
```

### Method 2: Paste and Press Ctrl+D (Unix/Mac) or Ctrl+Z (Windows)
```
> [Paste your text from clipboard - can be multiple lines]
> [Press Ctrl+D on Unix/Mac or Ctrl+Z then Enter on Windows]
```

### Method 3: Skip Input
```
> [Just press Enter immediately]
```

## Platform-Specific Instructions

### Windows
1. Paste your text (Ctrl+V or right-click)
2. Press Enter to go to a new line
3. Type `END` and press Enter

   **OR**

   Press Ctrl+Z then Enter (sends EOF signal)

### Mac/Linux
1. Paste your text (Cmd+V or Ctrl+V)
2. Press Enter to go to a new line
3. Type `END` and press Enter

   **OR**

   Press Ctrl+D (sends EOF signal)

## Example Use Cases

### Use Case 1: Paste Project Description
You have detailed project notes in another document:

```
Your context (or press Enter to skip):
> Led development of a microservices platform handling 50M+ requests/day
> Architected system using Go, Kubernetes, and PostgreSQL
> Achieved 99.99% uptime with <100ms p95 latency
> Mentored team of 5 junior developers
> Reduced infrastructure costs by 40% through optimization
> END
```

### Use Case 2: Paste Comprehensive Feedback
You have feedback notes prepared:

```
Your feedback:
> Make the letter more concise - target 3 short paragraphs
> Add specific metrics: 50M requests/day, 99.99% uptime, <100ms latency
> Emphasize the leadership aspect - mention mentoring 5 developers
> Use more active voice and action verbs
> Remove the generic "I am writing to express interest" opening
> Start with the impact I made instead
> END
```

### Use Case 3: Copy from Resume
Paste relevant section from your resume:

```
Your context:
> Senior Software Engineer | TechCorp (2020-2024)
> • Designed and built high-performance microservices platform
> • Handled 50M+ daily requests with 99.99% uptime
> • Led team of 5 engineers, implemented CI/CD pipeline
> • Reduced deployment time from 2 hours to 10 minutes
> • Tech stack: Go, Kubernetes, PostgreSQL, Redis, RabbitMQ
> END
```

## Tips for Best Results

### ✅ DO:
- Paste freely from any source (notes, resume, clipboard)
- Use multiple lines for detailed descriptions
- Include bullet points and formatting
- Copy/paste entire achievement lists
- Paste job-specific notes you've prepared

### ❌ DON'T:
- Worry about line breaks - they're preserved
- Try to format the text specially
- Manually type everything if you have it elsewhere
- Forget to type `END` or press Ctrl+D/Ctrl+Z when done

## Troubleshooting

### Input Not Working?
**Problem**: Nothing happens when you paste
**Solution**: Make sure you pressed Enter after pasting, then type `END` or press Ctrl+D/Ctrl+Z

### Only First Line Captured?
**Problem**: Multi-line paste only shows first line
**Solution**: Make sure you're using the `checkjd` command, not an older version. Rebuild with `go build -o jobseeker.exe ./cmd/jobseeker`

### EOF Signal Not Working?
**Windows**: Use `END` instead of Ctrl+Z if the EOF signal doesn't work
**Mac/Linux**: Type `END` instead of Ctrl+D if the EOF signal doesn't work

### Want to Start Over?
**Problem**: Made a mistake in your input
**Solution**: Just press Ctrl+C to cancel and start the command again

## Quick Reference

| Action | Windows | Mac/Linux |
|--------|---------|-----------|
| Paste text | Ctrl+V or Right-click | Cmd+V or Ctrl+V |
| End input (typed) | Type `END` + Enter | Type `END` + Enter |
| End input (signal) | Ctrl+Z + Enter | Ctrl+D |
| Skip input | Enter (immediately) | Enter (immediately) |
| Cancel | Ctrl+C | Ctrl+C |

## Examples from Real Usage

### Example 1: Initial Context with Achievements
```
> Built and deployed a cloud-native microservices architecture
> serving 10M+ users across 50+ countries
>
> Key achievements:
> - 99.99% uptime SLA maintained for 18 months
> - Reduced API response time from 500ms to <50ms
> - Cut infrastructure costs by 35% through container optimization
> - Led migration from monolith to microservices (6-month project)
>
> Tech stack: Go, Docker, Kubernetes, PostgreSQL, Redis, AWS
> END
```

### Example 2: Refinement with Multiple Changes
```
> Please make these changes to the cover letter:
>
> 1. Shorten to 3 paragraphs maximum (currently 4)
> 2. Add the specific metrics I provided about the 50M requests/day
> 3. Mention my leadership experience - I led a team of 8 developers
> 4. Change the tone to be more enthusiastic but still professional
> 5. Remove the generic opening, start with the impact I made
> 6. Emphasize the Go and Kubernetes experience more since it matches their stack
>
> END
```

## Benefits of Multi-line Input

1. **Save Time** - Copy/paste instead of typing
2. **More Detail** - Provide comprehensive context without typing limits
3. **Consistency** - Use the same prepared notes for multiple applications
4. **Flexibility** - Edit your notes externally and paste them in
5. **No Character Limits** - Paste as much as you need

## Related Commands

- `jobseeker checkjd` - Main command that uses multi-line input
- `jobseeker init` - Initialize your profile (single-line inputs only)
- `jobseeker analyze` - Analyze scraped jobs (no user input needed)
