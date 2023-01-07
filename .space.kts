/**
 * JetBrains Space Automation
 * This Kotlin-script file lets you automate build activities
 * For more info, see https://www.jetbrains.com/help/space/automation.html
 */

job("Build windows-amd64") {
    container(image = "golang:1.19-alpine") {
        shellScript {
            content = """
                echo Building
                GOOS=windows GOARCH=amd64 go build -o clay-relay.exe
                
                echo Done, uploading
                SOURCE_PATH=clay-relay.exe
                TARGET_PATH=clay-relay/builds/${'$'}JB_SPACE_EXECUTION_NUMBER/
                REPO_URL=https://files.pkg.jetbrains.space/npathy/p/clay/filesrepo
                curl -i -H "Authorization: Bearer ${'$'}JB_SPACE_CLIENT_TOKEN" -F file=@"${'$'}SOURCE_PATH" ${'$'}REPO_URL/${'$'}TARGET_PATH
            """.trimIndent()
        }
    }
}
