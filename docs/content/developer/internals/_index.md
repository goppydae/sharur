---
title: Internals
weight: 20
description: Architecture overview of gollm's components and data flow
---

This section describes the high-level architecture of `gollm`: how its components are organized, how data flows through the system, and how the key abstractions relate to each other.

---

## Directory Structure

```
gollm/
│   ├── internal/
│   │   ├── service/        # Central AgentService implementation + in-process client
│   │   ├── gen/            # Generated Protobuf stubs (pb.AgentServiceClient/Server)
│   │   ├── agent/          # Core agentic loop, event bus, state machine
│   │   ├── llm/            # LLM provider adapters (Ollama, OpenAI, Anthropic, llama.cpp, Google)
│   │   ├── tools/          # Built-in tool implementations + registry
│   │   ├── session/        # JSONL-backed session persistence, branching, tree
│   │   ├── modes/
│   │   │   ├── interactive/ # Bubble Tea TUI (pb client)
│   │   │   ├── print.go    # One-shot CLI JSONL mode (pb client)
│   │   │   └── grpc.go     # gRPC server mode (wraps Service)
│   │   ├── config/         # Config loading (global + project layering)
│   │   ├── themes/         # TUI colour themes
│   │   ├── types/          # Shared value types (Message, Session, ThinkingLevel)
│   │   ├── events/         # Generic publish-subscribe event bus
│   │   ├── skills/         # Skill discovery (Markdown files → slash commands)
│   │   ├── prompts/        # Prompt template discovery
│   │   └── contextfiles/   # Auto-discovered context file injection (AGENTS.md, etc.)
│   ├── cmd/                # Entry points (glm)
│   ├── proto/              # Protobuf definitions (gollm/v1/agent.proto)
│   ├── extensions/         # gRPC extension loader + proto definitions
│   └── sdk/                # Public Go SDK
```

---

## Component Diagram

```mermaid
flowchart TD
    CLI["CLI flags & Config"] --> Svc

    subgraph core ["internal/agent"]
        Agent["Agent
Messages · SteerQueue · FollowUpQueue
StateMachine"]
        RunTurn["runTurn
provider.Stream · consumeStream · execTools"]
        EB["EventBus
async · non-blocking · 4096-item buffer"]
        Agent --> RunTurn
        RunTurn -->|publishes| EB
    end

    Svc["internal/service
AgentService"] --> core

    RunTurn --> LLM

    subgraph llm ["internal/llm"]
        LLM["Provider interface
Stream · Info"]
        Adapters["Ollama · OpenAI · Anthropic
llama.cpp · Google"]
        LLM --> Adapters
    end

    EB --> TUI["TUI"]
    EB --> JSON["JSON stdout"]
    EB --> GRPC["gRPC stream"]
    EB --> Session["session saver"]
```

---

## Data Flow Summary

```mermaid
flowchart TD
    Input["User Input"] --> Mode["TUI · JSON · Remote Client"]
    Mode --> PBClient["pb.AgentServiceClient
bufconn or TCP"]
    PBClient --> Service["internal/service
getOrCreate / loadIfExists"]
    Service --> AP["agent.Prompt(ctx, text)"]
    AP --> MI["ext.ModifyInput()"]
    MI --> SS["ext.SessionStart() · ext.AgentStart()
EventAgentStart"]

    SS --> Loop

    subgraph Loop ["runTurn loop"]
        direction TB
        BP["ext.BeforePrompt() · ModifySystemPrompt()
ModifyContext() · BeforeProviderRequest()"]
        LLMStream["llm.Provider.Stream()
EventTextDelta · EventThinkingDelta · EventToolCall"]
        APR["ext.AfterProviderResponse()
EventTurnStart · ext.TurnStart()"]
        ToolExec["ext.BeforeToolCall() · execTool() · ext.AfterToolCall()
EventToolDelta · EventToolOutput"]
        TE["ext.TurnEnd()"]
        More{"more tool calls?"}
        BP --> LLMStream --> APR --> ToolExec --> TE --> More
        More -->|yes| BP
    end

    More -->|no| AgEnd["EventAgentEnd · ext.AgentEnd()"]
    AgEnd --> Save["service saves session to disk"]
    Save --> Stream["Stream Protobuf Events to client"]
    Stream --> Render["Render: TUI · JSONL stdout · gRPC stream"]
```
