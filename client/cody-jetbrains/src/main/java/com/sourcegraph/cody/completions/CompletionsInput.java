package com.sourcegraph.cody.completions;

import org.jetbrains.annotations.NotNull;

import java.util.List;

/**
 * Input for the completions request.
 */
public class CompletionsInput {
    public @NotNull List<Message> messages;
    public float temperature;
    public int maxTokensToSample;
    public int topK;
    public int topP;

    public CompletionsInput(@NotNull List<Message> messages, float temperature, int maxTokensToSample, int topK, int topP) {
        this.messages = messages;
        this.temperature = temperature;
        this.maxTokensToSample = maxTokensToSample;
        this.topK = topK;
        this.topP = topP;
    }

    public void addMessage(@NotNull Speaker speaker, @NotNull String text) {
        messages.add(new Message(speaker, text));
    }
}
