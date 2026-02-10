#!/bin/bash

# AI Gateway - Environment Setup Script
# This script helps you set up environment variables before running docker-compose

set -e

ENV_FILE=".env"
ENV_EXAMPLE=".env.example"

echo "🚀 AI Gateway - Environment Setup"
echo "=================================="
echo ""

# Check if .env already exists
if [ -f "$ENV_FILE" ]; then
    echo "⚠️  .env file already exists!"
    read -p "Do you want to overwrite it? (y/N): " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "✅ Using existing .env file"
        exit 0
    fi
fi

# Copy from example
if [ -f "$ENV_EXAMPLE" ]; then
    cp "$ENV_EXAMPLE" "$ENV_FILE"
    echo "✅ Created .env from .env.example"
else
    echo "❌ .env.example not found!"
    exit 1
fi

echo ""
echo "📝 Please provide your API keys:"
echo ""

# Prompt for OpenAI API Key
read -p "Enter your OpenAI API Key (or press Enter to skip): " openai_key
if [ ! -z "$openai_key" ]; then
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        sed -i '' "s|^OPENAI_API_KEY=.*|OPENAI_API_KEY=$openai_key|" "$ENV_FILE"
    else
        # Linux
        sed -i "s|^OPENAI_API_KEY=.*|OPENAI_API_KEY=$openai_key|" "$ENV_FILE"
    fi
    echo "✅ OpenAI API Key set"
fi

# Prompt for Anthropic API Key
read -p "Enter your Anthropic API Key (or press Enter to skip): " anthropic_key
if [ ! -z "$anthropic_key" ]; then
    if [[ "$OSTYPE" == "darwin"* ]]; then
        # macOS
        sed -i '' "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=$anthropic_key|" "$ENV_FILE"
    else
        # Linux
        sed -i "s|^ANTHROPIC_API_KEY=.*|ANTHROPIC_API_KEY=$anthropic_key|" "$ENV_FILE"
    fi
    echo "✅ Anthropic API Key set"
fi

echo ""
echo "✅ Environment setup complete!"
echo ""
echo "📋 Next steps:"
echo "   1. Review/edit .env file if needed: nano .env"
echo "   2. Start the gateway: docker-compose up"
echo ""
