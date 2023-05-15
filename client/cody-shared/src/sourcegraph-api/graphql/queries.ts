export const CURRENT_USER_ID_QUERY = `
query CurrentUser {
    currentUser {
        id
    }
}`

// TODO: We can only query these fields on dotcom ... instrospection query?
export const AUTH_STATUS_QUERY = `
query CurrentUser($siteHasRequiresVerifiedEmailField: Boolean!) {
	site {
		requiresVerifiedEmailForCody @include(if: $withFriends)
	}
    currentUser {
        id
		hasVerifiedEmail
    }
}`

export const SITE_FIELD_NAMES_QUERY = `
query {
  __type(name: "Site") {
    fields {
      name
    }
  }
}`

export const REPOSITORY_ID_QUERY = `
query Repository($name: String!) {
	repository(name: $name) {
		id
	}
}`

export const REPOSITORY_EMBEDDING_EXISTS_QUERY = `
query Repository($name: String!) {
	repository(name: $name) {
                id
                embeddingExists
	}
}`

export const SEARCH_EMBEDDINGS_QUERY = `
query EmbeddingsSearch($repo: ID!, $query: String!, $codeResultsCount: Int!, $textResultsCount: Int!) {
	embeddingsSearch(repo: $repo, query: $query, codeResultsCount: $codeResultsCount, textResultsCount: $textResultsCount) {
		codeResults {
			fileName
			startLine
			endLine
			content
		}
		textResults {
			fileName
			startLine
			endLine
			content
		}
	}
}`

export const IS_CONTEXT_REQUIRED_QUERY = `
query IsContextRequiredForChatQuery($query: String!) {
	isContextRequiredForChatQuery(query: $query)
}`

export const LOG_EVENT_MUTATION = `
mutation LogEventMutation($event: String!, $userCookieID: String!, $url: String!, $source: EventSource!, $argument: String, $publicArgument: String) {
    logEvent(
		event: $event
		userCookieID: $userCookieID
		url: $url
		source: $source
		argument: $argument
		publicArgument: $publicArgument
    ) {
		alwaysNil
	}
}`
