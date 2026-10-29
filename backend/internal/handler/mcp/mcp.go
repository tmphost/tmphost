// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package mcp is the stateless MCP (streamable HTTP) transport. It offers
// one tool per operation (uploadFile, downloadFile, listFiles, getUrl,
// extendFolder, addPinToFolder), mirroring the `tmphost file` CLI commands; each
// takes only the arguments its operation needs. It also serves the tmphost
// Agent Skill through the MCP Skills extension (skills.go).
package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/tmphost/backend/internal/brand"
	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
)

// The tools' names.
const (
	ToolUploadFile     = "uploadFile"
	ToolDownloadFile   = "downloadFile"
	ToolListFiles      = "listFiles"
	ToolGetURL         = "getUrl"
	ToolExtendFolder   = "extendFolder"
	ToolAddPinToFolder = "addPinToFolder"
)

// The content formats: which field carries a file's content.
const (
	FormatBase64 = "base64Content"
	FormatText   = "textContent"
)

const (
	_uploadFileDescription     = `Store a file in a temporary folder and get its public URL. Without folderId a new folder is created that lives for ttl (default 30m); pin true gives it a 6 digit PIN visitors must enter. With folderId the file goes into that folder, and a file already at that path is replaced in place (its url stays the same). Put the content in base64Content or, for UTF-8 text with format textContent, in textContent. Returns folderId and the file's url; when this call created the folder, also expiresAt, pinMode if a PIN guards it, and pin if this call made the folder's own PIN. Costs 1 credit per started MB of the file per started 30 minutes of the folder's remaining lifetime.`
	_downloadFileDescription   = `Read back the content of a file in one of your folders, such as to check what was uploaded or to edit a page and upload it again with uploadFile (which replaces it at the same url). It costs no credits and changes nothing. To share a file, give its url instead (from uploadFile, listFiles or getUrl). Name the file by fileName and path as listFiles returns them. Returns the file's folderId, fileName, path and size in bytes, with its content in base64Content (any file) or, with format textContent, in textContent as UTF-8 text; textContent fails for a file that is not UTF-8, such as an image, so use base64Content for those.`
	_listFilesDescription      = `List the files in one of your folders. Returns the folder's url, expiresAt and pinMode (when a PIN guards it), and files: one entry per file, sorted by full path, with its fileName, the directory path that holds it, its size in bytes, its public url and the agent that last wrote it. Pass a file's fileName and path to downloadFile or getUrl.`
	_getURLDescription         = `Build the public URL of one of your folders, a directory in it (path) or a file in it (fileName, optional path) without uploading anything; it does not check that the directory or file exists (use listFiles for that). Returns the url, the folder's folderId and expiresAt (the link stops working then), the fileName and path it was built for, and pinMode when a PIN guards the folder. Pass the pin from uploadFile or addPinToFolder to get a url that carries it as ?pin=, so visitors skip the PIN page; the server keeps only a hash of a PIN, so it cannot put one in the url by itself, and it does not check the pin you pass. Fails with pin when no PIN guards the folder or it is guarded by an authenticator (totp) code.`
	_extendFolderDescription   = `Make a folder expire later by extendBy, given in whole minutes such as 1h; its remaining lifetime can't exceed the max TTL. Returns the folder's url, its new expiresAt and the creditsSpent on this call. Costs, for each file in the folder, 1 credit per started MB per started 30 minutes of the added time; an empty folder is free. Fails, adding no time, when the user lacks the credits.`
	_addPinToFolderDescription = `Give one of your folders a new 6 digit PIN that visitors must enter before they see its files; it replaces the folder's old PIN. Returns the new pin (shown only this once: the server keeps just a hash, so give it to the user now or pass it to getUrl), the folder's url and expiresAt, and pinMode folder. Fails when the owner's account PIN already guards every folder.`
)

// _instructions tell an agent when and how to use the server.
const _instructions = `tmphost.io hosts files for a limited time: use it to share a file, a report or a whole static site at a public URL that expires on its own.
- Call uploadFile without folderId for the first file to create a folder (set ttl for how long it lives, 30m by default), then pass the returned folderId to add more files; keep paths relative so pages link to each other. A folder serves index.html at its root.
- Prefer format textContent for UTF-8 text (HTML, CSS, JS, JSON, Markdown, CSV) and base64Content for anything else.
- Uploading to a path that already holds a file replaces it in place, and its URL stays the same.
- uploadFile and extendFolder spend the user's credits by size and lifetime, so pick the shortest ttl that fits and don't re-upload unchanged files.
- Anything uploaded is public to whoever has the URL until the folder expires: never upload secrets or private data unless the user asks to.
- Give the user the url from the result; use listFiles to list a folder, downloadFile to read a file back, and getUrl to build links without uploading.
- To share a Markdown file (.md) as a readable web page, give its url with ?format=html appended.
- A folder can be guarded by a 6 digit PIN that visitors enter once (pinMode in the result). When the result carries pin, give it to the user with the url: it is shown only once. A link with ?pin=<pin> appended opens the folder without asking (getUrl with pin builds one; the server cannot recover a PIN later). Pass pin true to the first uploadFile, or call addPinToFolder, when the user wants the files kept from anyone without the PIN.
- The skill at skill://tmphost/SKILL.md (skills/list, resources/read) has the full best practices: load it before a multi-file site, a PIN-guarded folder or an update to shared files.`

type (
	// Params are the transport's dependencies.
	Params struct {
		Folders folderctrl.Controller
		Auth    authctrl.Controller
		// MaxBodyBytes caps a request; uploads arrive base64-encoded inside
		// it.
		MaxBodyBytes int64
		// ResourceMetadataURL is where clients without a token learn how
		// to get one (RFC 9728).
		ResourceMetadataURL string
		// EndpointURL is the endpoint's public URL, which the setup page
		// shows people who open it in a browser.
		EndpointURL string
	}

	// UploadFileInput is uploadFile's arguments.
	UploadFileInput struct {
		FolderID string `json:"folderId,omitempty" jsonschema:"folder to add the file to or replace it in; leave out to create a new folder"`
		FileName string `json:"fileName" jsonschema:"file name, without directories, such as index.html"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder, such as site/css, at most 5 levels; empty for the folder root"`
		Format   string `json:"format,omitempty" jsonschema:"which field carries the content: base64Content (default, any file) or textContent (UTF-8 text)"`
		// Base64Content and TextContent are pointers so an empty file can
		// be told apart from a missing field.
		Base64Content *string `json:"base64Content,omitempty" jsonschema:"base64 file content, with format base64Content"`
		TextContent   *string `json:"textContent,omitempty" jsonschema:"UTF-8 text file content, with format textContent"`
		TTL           string  `json:"ttl,omitempty" jsonschema:"new folder only: how long it lives, such as 30m or 2h (default 30m)"`
		Pin           bool    `json:"pin,omitempty" jsonschema:"new folder only: give it a 6 digit PIN visitors must enter"`
	}

	// UploadFileOutput is uploadFile's result.
	UploadFileOutput struct {
		FolderID     string `json:"folderId" jsonschema:"id of the folder that holds the file, newly created or as passed in; pass it as folderId to add more files or to the other tools"`
		URL          string `json:"url" jsonschema:"the file's public URL, working until the folder expires; give it to the user"`
		FileName     string `json:"fileName" jsonschema:"the file's name, as passed in"`
		Path         string `json:"path" jsonschema:"the directory inside the folder that holds the file, without leading or trailing slashes; empty for the folder root"`
		Size         int64  `json:"size" jsonschema:"the stored file's size in bytes"`
		CreditsSpent int    `json:"creditsSpent" jsonschema:"credits this call took from the user's balance"`
		// ExpiresAt is set when this call created the folder.
		ExpiresAt string `json:"expiresAt,omitempty" jsonschema:"when the new folder expires and its URLs stop working, as an RFC 3339 UTC time; absent when the file went into an existing folder (listFiles or getUrl return its expiry)"`
		// PinMode names the PIN visitors must enter: totp or static for
		// the owner's account PIN, folder for the folder's own.
		PinMode string `json:"pinMode,omitempty" jsonschema:"set when this call created the folder and a PIN guards it: folder for the folder's own PIN, static or totp for the owner's account PIN; absent otherwise"`
		// Pin is the folder's own PIN, only from the call that made it.
		Pin string `json:"pin,omitempty" jsonschema:"the new folder's own 6 digit PIN, when this call made one (pinMode folder); give it to the user with the url, it cannot be read back later"`
	}

	// DownloadFileInput is downloadFile's arguments.
	DownloadFileInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile returned it in folderId, such as 1a2b3c4d"`
		FileName string `json:"fileName" jsonschema:"file name, without directories, such as index.html"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder that holds the file, as listFiles returns it in path, such as site/css; leave out for a file at the folder root"`
		Format   string `json:"format,omitempty" jsonschema:"which field carries the content: base64Content (default, any file) or textContent (UTF-8 text)"`
	}

	// DownloadFileOutput is downloadFile's result.
	DownloadFileOutput struct {
		FolderID string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		FileName string `json:"fileName" jsonschema:"the file's name, as passed in"`
		Path     string `json:"path" jsonschema:"the directory inside the folder that holds the file; empty for the folder root"`
		Size     int64  `json:"size" jsonschema:"the file's size in bytes"`
		// Base64Content and TextContent are pointers so an empty file
		// still carries its (empty) content field.
		Base64Content *string `json:"base64Content,omitempty" jsonschema:"the file's content, base64-encoded; set when format is base64Content"`
		TextContent   *string `json:"textContent,omitempty" jsonschema:"the file's content as UTF-8 text; set when format is textContent"`
	}

	// FolderInput is the arguments of the tools that take only a folder.
	FolderInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile returned it in folderId, such as 1a2b3c4d"`
	}

	// ListFilesOutput is listFiles' result.
	ListFilesOutput struct {
		FolderID  string     `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL       string     `json:"url" jsonschema:"the folder's public URL, ending with a slash; it serves index.html when the folder has one at its root"`
		ExpiresAt string     `json:"expiresAt" jsonschema:"when the folder and its files expire, as an RFC 3339 UTC time"`
		PinMode   string     `json:"pinMode,omitempty" jsonschema:"the PIN visitors must enter: folder for the folder's own PIN, static or totp for the owner's account PIN; absent when no PIN guards the folder"`
		Files     []FileInfo `json:"files" jsonschema:"the folder's files, one entry per file sorted by full path; an empty array, never null, when the folder has none"`
	}

	// FileInfo is a file in a listFiles listing.
	FileInfo struct {
		FileName string `json:"fileName" jsonschema:"the file's name, without directories"`
		Path     string `json:"path" jsonschema:"the directory inside the folder that holds the file, such as site/css; empty for the folder root"`
		Size     int64  `json:"size" jsonschema:"the file's size in bytes"`
		URL      string `json:"url" jsonschema:"the file's public URL"`
		// Agent names what last wrote the file.
		Agent string `json:"agent,omitempty" jsonschema:"what last wrote the file: the OAuth client's name, or Web for the web app; absent when unknown"`
	}

	// GetURLInput is getUrl's arguments.
	GetURLInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile returned it in folderId, such as 1a2b3c4d"`
		Path     string `json:"path,omitempty" jsonschema:"directory inside the folder to link to, or that holds fileName, as listFiles returns it in path, such as site/css; leave out for the folder root"`
		FileName string `json:"fileName,omitempty" jsonschema:"name of the file to link to, as listFiles returns it in fileName, such as index.html; leave out for the URL of the folder or of path"`
		Pin      string `json:"pin,omitempty" jsonschema:"the folder's own or account PIN, as uploadFile or addPinToFolder returned it; the url then carries it as ?pin= so visitors skip the PIN page. Only for pinMode folder or static"`
	}

	// GetURLOutput is getUrl's result.
	GetURLOutput struct {
		FolderID  string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL       string `json:"url" jsonschema:"the public URL of the folder, directory or file, with ?pin= when pin was passed; a folder or directory URL ends with a slash"`
		ExpiresAt string `json:"expiresAt" jsonschema:"when the folder expires and the url stops working, as an RFC 3339 UTC time"`
		FileName  string `json:"fileName,omitempty" jsonschema:"the file name the url points to, as passed in; absent for a folder or directory URL"`
		Path      string `json:"path,omitempty" jsonschema:"the directory the url points into, without leading or trailing slashes; absent for the folder root"`
		// PinMode names the PIN that guards the folder, as in listFiles.
		PinMode string `json:"pinMode,omitempty" jsonschema:"the PIN visitors must enter: folder for the folder's own PIN, static or totp for the owner's account PIN; absent when no PIN guards the folder"`
	}

	// ExtendFolderInput is extendFolder's arguments.
	ExtendFolderInput struct {
		FolderID string `json:"folderId" jsonschema:"id of a folder you own, as uploadFile returned it in folderId, such as 1a2b3c4d"`
		ExtendBy string `json:"extendBy" jsonschema:"how much longer the folder should live, added to its current expiry, such as 30m, 1h or 1h30m; the credits charged grow with this added time"`
	}

	// ExtendFolderOutput is extendFolder's result.
	ExtendFolderOutput struct {
		FolderID     string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL          string `json:"url" jsonschema:"the folder's public URL, the same as before the extension; its links keep working until expiresAt"`
		ExpiresAt    string `json:"expiresAt" jsonschema:"the folder's new expiry, its old one plus extendBy, as an RFC 3339 UTC time; tell the user the files stay up until then"`
		CreditsSpent int    `json:"creditsSpent" jsonschema:"credits this call took from the user's balance for the added time; 0 for an empty folder"`
	}

	// AddPinToFolderOutput is addPinToFolder's result.
	AddPinToFolderOutput struct {
		FolderID  string `json:"folderId" jsonschema:"the folder's id, as passed in"`
		URL       string `json:"url" jsonschema:"the folder's public URL, unchanged; visitors now get a PIN page there unless the link carries ?pin=<pin> (getUrl with pin builds one)"`
		ExpiresAt string `json:"expiresAt" jsonschema:"when the folder expires, as an RFC 3339 UTC time; adding a PIN does not change it"`
		PinMode   string `json:"pinMode" jsonschema:"always folder: the folder's own PIN now guards it, as listFiles and getUrl will report"`
		Pin       string `json:"pin" jsonschema:"the new 6 digit PIN visitors must enter; any older PIN of the folder stops working. Give it to the user with the url now: the server keeps only a hash and cannot return it again"`
	}

	tools struct {
		folders folderctrl.Controller
	}

	// caller is who calls a tool: the token's user and OAuth client.
	caller struct {
		userID int64
		agent  string
	}
)

// New returns the MCP endpoint. Every request needs an OAuth access token
// as a Bearer token; without one, the 401 points to the resource metadata.
func New(p Params) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{
		Name:        "tmphost",
		Title:       "tmphost.io",
		Version:     brand.Version,
		Description: "Temporary file hosting: upload files and static sites to public URLs that expire.",
		WebsiteURL:  "https://tmphost.io",
		Icons:       icons(),
	}, &mcp.ServerOptions{Instructions: _instructions, Capabilities: capabilities()})
	t := &tools{folders: p.Folders}
	mcp.AddTool(server, &mcp.Tool{Name: ToolUploadFile, Title: "Upload a file", Description: _uploadFileDescription, InputSchema: schemaFor[UploadFileInput](), OutputSchema: schemaFor[UploadFileOutput](), Annotations: writeAnnotations("Upload a file", true, true), Icons: icons()}, handle(t.uploadFile))
	mcp.AddTool(server, &mcp.Tool{Name: ToolDownloadFile, Title: "Download a file", Description: _downloadFileDescription, InputSchema: schemaFor[DownloadFileInput](), OutputSchema: schemaFor[DownloadFileOutput](), Annotations: readAnnotations("Download a file"), Icons: icons()}, handle(t.downloadFile))
	mcp.AddTool(server, &mcp.Tool{Name: ToolListFiles, Title: "List a folder's files", Description: _listFilesDescription, InputSchema: schemaFor[FolderInput](), OutputSchema: listFilesSchema(), Annotations: readAnnotations("List a folder's files"), Icons: icons()}, handle(t.listFiles))
	mcp.AddTool(server, &mcp.Tool{Name: ToolGetURL, Title: "Get a public URL", Description: _getURLDescription, InputSchema: schemaFor[GetURLInput](), OutputSchema: schemaFor[GetURLOutput](), Annotations: readAnnotations("Get a public URL"), Icons: icons()}, handle(t.getURL))
	mcp.AddTool(server, &mcp.Tool{Name: ToolExtendFolder, Title: "Extend a folder", Description: _extendFolderDescription, InputSchema: schemaFor[ExtendFolderInput](), OutputSchema: schemaFor[ExtendFolderOutput](), Annotations: writeAnnotations("Extend a folder", false, true), Icons: icons()}, handle(t.extendFolder))
	mcp.AddTool(server, &mcp.Tool{Name: ToolAddPinToFolder, Title: "Add a PIN to a folder", Description: _addPinToFolderDescription, InputSchema: schemaFor[FolderInput](), OutputSchema: schemaFor[AddPinToFolderOutput](), Annotations: writeAnnotations("Add a PIN to a folder", true, false), Icons: icons()}, handle(t.addPinToFolder))
	_skills.register(server)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: p.MaxBodyBytes,
	})
	return setupPage(renderSetupPage(p.EndpointURL), auth.RequireBearerToken(verifier(p.Auth), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    p.ResourceMetadataURL,
		AllowMissingExpiration: true,
	})(negotiate(handler, p.MaxBodyBytes)))
}

// capabilities advertise a stateless server: tools, and the skill files as
// resources with the Skills extension, but no notifications, as there is no
// session to send them on (no logging, and the tool and resource lists never
// change while the server runs). Every protocol version
// the SDK speaks is offered, newest first, so a client that supports the
// stateless 2026-07-28 protocol (server/discover, no initialize handshake)
// gets it, and older clients negotiate their own.
func capabilities() *mcp.ServerCapabilities {
	caps := &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: false}, Resources: &mcp.ResourceCapabilities{ListChanged: false}}
	caps.AddExtension(ExtensionSkills, map[string]any{"directoryRead": true})
	return caps
}

// _skills are the built-in skills, loaded once.
var _skills = mustLoadSkills(_skillFiles)

// _extraAgent is the token info Extra key that names the OAuth client.
const _extraAgent = "agent"

// verifier accepts access tokens; the user ID and the client's name
// travel in the token info.
func verifier(controller authctrl.Controller) auth.TokenVerifier {
	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		rs, err := controller.Authenticate(ctx, &entity.AuthenticateRequest{AccessToken: token})
		if err != nil {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: monoflake.ID(rs.UserID).String(), Extra: map[string]any{_extraAgent: rs.Agent}}, nil
	}
}

// icons are the tmphost.io icon as data URIs, so they show whichever host
// the server is reached on: the SVG for any size, and a PNG for clients that
// don't draw SVG.
func icons() []mcp.Icon {
	return []mcp.Icon{
		{Source: brand.DataURI(brand.IconSVGType, brand.IconSVG), MIMEType: brand.IconSVGType, Sizes: []string{"any"}},
		{Source: brand.DataURI(brand.IconPNGType, brand.IconPNG), MIMEType: brand.IconPNGType, Sizes: []string{"128x128"}},
	}
}

// readAnnotations describe a tool that only reads the caller's folders.
// destructiveHint is set to false rather than left out: platform checks want
// every hint declared as a boolean.
func readAnnotations(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true, OpenWorldHint: new(false)}
}

// writeAnnotations describe a tool that changes a folder and is not safe to
// repeat. destructive: it can replace what is there; openWorld: it
// publishes to, or keeps on, the open web.
func writeAnnotations(title string, destructive, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: new(destructive), OpenWorldHint: new(openWorld)}
}

// listFilesSchema is listFiles' output schema, with each files entry
// described too.
func listFilesSchema() *jsonschema.Schema {
	s := schemaFor[ListFilesOutput]()
	// singleTypes moved the array into the second anyOf branch.
	s.Properties["files"].AnyOf[1].Items.Description = "one file in the folder: pass its fileName and path to downloadFile or getUrl, or give its url to the user"
	return s
}

// schemaFor infers a tool's input or output schema, limiting format to the
// known content formats and spelling nullable fields portably.
func schemaFor[T any]() *jsonschema.Schema {
	// The inputs and outputs are plain structs, so inference cannot fail.
	s, _ := jsonschema.For[T](nil)
	if f := s.Properties["format"]; f != nil {
		f.Enum = []any{FormatBase64, FormatText}
	}
	singleTypes(s)
	return s
}

// singleTypes rewrites every `type` array the inference makes for pointers
// and slices, such as ["null","string"], into anyOf branches of one type
// each. Clients that map tool schemas onto a single-type dialect (like
// Gemini's OpenAPI subset) refuse or mishandle type arrays.
func singleTypes(s *jsonschema.Schema) {
	for _, p := range s.Properties {
		singleTypes(p)
	}
	if s.Items != nil {
		singleTypes(s.Items)
	}
	if len(s.Types) < 2 {
		return
	}
	for _, typ := range s.Types {
		branch := &jsonschema.Schema{Type: typ}
		if typ == "array" {
			branch.Items = s.Items
		}
		s.AnyOf = append(s.AnyOf, branch)
	}
	s.Types, s.Items = nil, nil
}

// handle adapts a tool method to the SDK, passing it the caller from the
// token.
func handle[In, Out any](fn func(context.Context, caller, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		agent, _ := req.Extra.TokenInfo.Extra[_extraAgent].(string)
		out, err := fn(ctx, caller{userID: monoflake.IDFromBase62(req.Extra.TokenInfo.UserID).Int64(), agent: agent}, in)
		return nil, out, err
	}
}

func (t *tools) uploadFile(ctx context.Context, c caller, in UploadFileInput) (UploadFileOutput, error) {
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return UploadFileOutput{}, err
	}
	data, err := uploadContent(in)
	if err != nil {
		return UploadFileOutput{}, err
	}
	folderID, created, err := t.uploadFolder(ctx, c, in)
	if err != nil {
		return UploadFileOutput{}, err
	}
	rs, err := t.folders.PutFiles(ctx, &entity.PutFilesRequest{UserID: c.userID, Agent: c.agent, FolderID: folderID, Files: []entity.FileUpload{{Path: p, Data: data}}})
	if err != nil {
		if created != nil {
			// Don't leave an empty folder behind for a failed first upload.
			_ = t.folders.DeleteFolder(ctx, &entity.DeleteFolderRequest{UserID: c.userID, FolderID: folderID})
		}
		return UploadFileOutput{}, err
	}
	out := UploadFileOutput{
		FolderID:     idgen.FormatFolderID(folderID),
		URL:          rs.Files[0].URL,
		FileName:     in.FileName,
		Path:         dirOf(p),
		Size:         rs.Files[0].Size,
		CreditsSpent: rs.CreditsSpent,
	}
	if created != nil {
		out.ExpiresAt = formatTime(created.ExpiresAt)
		out.PinMode, out.Pin = created.PinMode, created.Pin
	}
	return out, nil
}

// uploadContent decodes the content field that format names. The other
// field must be empty, so content is never silently ignored.
func uploadContent(in UploadFileInput) ([]byte, error) {
	format, err := contentFormat(in.Format)
	if err != nil {
		return nil, err
	}
	if format == FormatText {
		if in.Base64Content != nil {
			return nil, errors.New("base64Content needs format base64Content")
		}
		if in.TextContent == nil {
			return nil, errors.New("textContent is required with format textContent")
		}
		return []byte(*in.TextContent), nil
	}
	if in.TextContent != nil {
		return nil, errors.New("textContent needs format textContent")
	}
	if in.Base64Content == nil {
		return nil, errors.New("base64Content is required with format base64Content")
	}
	data, err := base64.StdEncoding.DecodeString(*in.Base64Content)
	if err != nil {
		return nil, errors.New("base64Content must be base64")
	}
	return data, nil
}

// contentFormat checks a format, defaulting to base64Content.
func contentFormat(format string) (string, error) {
	switch format {
	case "", FormatBase64:
		return FormatBase64, nil
	case FormatText:
		return FormatText, nil
	default:
		return "", fmt.Errorf("unknown format %q: use base64Content or textContent", format)
	}
}

// uploadFolder resolves the folder an upload goes to, creating it when no
// folderId is given. created is the new folder, if any.
func (t *tools) uploadFolder(ctx context.Context, c caller, in UploadFileInput) (int64, *entity.Folder, error) {
	if in.FolderID != "" {
		if in.TTL != "" || in.Pin {
			return 0, nil, errors.New("ttl and pin only apply to a new folder: leave out folderId, or use extendFolder or addPinToFolder")
		}
		id, err := folderID(in.FolderID)
		return id, nil, err
	}
	minutes := 0
	if in.TTL != "" {
		var err error
		if minutes, err = durationMinutes("ttl", in.TTL); err != nil {
			return 0, nil, err
		}
	}
	rs, err := t.folders.CreateFolder(ctx, &entity.CreateFolderRequest{UserID: c.userID, Agent: c.agent, TTLMinutes: minutes, Pin: in.Pin})
	if err != nil {
		return 0, nil, err
	}
	return rs.Folder.ID, &rs.Folder, nil
}

// durationMinutes parses a duration argument, such as 30m or 2h.
func durationMinutes(name, raw string) (int, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d%time.Minute != 0 {
		return 0, fmt.Errorf("%s must be whole minutes, such as 30m or 2h", name)
	}
	return int(d / time.Minute), nil
}

func (t *tools) extendFolder(ctx context.Context, c caller, in ExtendFolderInput) (ExtendFolderOutput, error) {
	id, err := folderID(in.FolderID)
	if err != nil {
		return ExtendFolderOutput{}, err
	}
	if in.ExtendBy == "" {
		return ExtendFolderOutput{}, errors.New("extendBy is required: the time to add, such as 1h")
	}
	minutes, err := durationMinutes("extendBy", in.ExtendBy)
	if err != nil {
		return ExtendFolderOutput{}, err
	}
	rs, err := t.folders.ExtendFolder(ctx, &entity.ExtendFolderRequest{UserID: c.userID, Agent: c.agent, FolderID: id, Minutes: minutes})
	if err != nil {
		return ExtendFolderOutput{}, err
	}
	return ExtendFolderOutput{FolderID: in.FolderID, URL: rs.Folder.URL, ExpiresAt: formatTime(rs.Folder.ExpiresAt), CreditsSpent: rs.CreditsSpent}, nil
}

func (t *tools) addPinToFolder(ctx context.Context, c caller, in FolderInput) (AddPinToFolderOutput, error) {
	id, err := folderID(in.FolderID)
	if err != nil {
		return AddPinToFolderOutput{}, err
	}
	rs, err := t.folders.CreateFolderPin(ctx, &entity.FolderPinRequest{UserID: c.userID, FolderID: id})
	if err != nil {
		return AddPinToFolderOutput{}, err
	}
	return AddPinToFolderOutput{FolderID: in.FolderID, URL: rs.Folder.URL, ExpiresAt: formatTime(rs.Folder.ExpiresAt), PinMode: rs.Folder.PinMode, Pin: rs.Folder.Pin}, nil
}

func (t *tools) downloadFile(ctx context.Context, c caller, in DownloadFileInput) (DownloadFileOutput, error) {
	format, err := contentFormat(in.Format)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	id, err := folderID(in.FolderID)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	p, err := filePath(in.Path, in.FileName)
	if err != nil {
		return DownloadFileOutput{}, err
	}
	rs, err := t.folders.OpenFile(ctx, &entity.OpenFileRequest{UserID: c.userID, FolderID: id, Path: p})
	if err != nil {
		return DownloadFileOutput{}, err
	}
	defer func() { _ = rs.Body.Close() }()
	data, err := io.ReadAll(rs.Body)
	if err != nil {
		return DownloadFileOutput{}, fmt.Errorf("read file: %w", err)
	}
	out := DownloadFileOutput{FolderID: in.FolderID, FileName: in.FileName, Path: dirOf(p), Size: int64(len(data))}
	if format == FormatText {
		if !utf8.Valid(data) {
			return DownloadFileOutput{}, errors.New("the file is not UTF-8 text: download it with format base64Content")
		}
		text := string(data)
		out.TextContent = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(data)
		out.Base64Content = &encoded
	}
	return out, nil
}

func (t *tools) listFiles(ctx context.Context, c caller, in FolderInput) (ListFilesOutput, error) {
	rs, err := t.folder(ctx, c, in.FolderID)
	if err != nil {
		return ListFilesOutput{}, err
	}
	files := make([]FileInfo, len(rs.Files))
	for i, f := range rs.Files {
		files[i] = FileInfo{FileName: f.Path[strings.LastIndex(f.Path, "/")+1:], Path: dirOf(f.Path), Size: f.Size, URL: f.URL, Agent: f.Agent}
	}
	return ListFilesOutput{FolderID: in.FolderID, URL: rs.Folder.URL, ExpiresAt: formatTime(rs.Folder.ExpiresAt), PinMode: rs.Folder.PinMode, Files: files}, nil
}

func (t *tools) getURL(ctx context.Context, c caller, in GetURLInput) (GetURLOutput, error) {
	dir := strings.Trim(in.Path, "/")
	full := strings.Trim(dir+"/"+in.FileName, "/")
	if strings.Contains(in.FileName, "/") || (full != "" && !folderctrl.ValidPath(full)) {
		return GetURLOutput{}, folderctrl.ErrInvalidPath
	}
	rs, err := t.folder(ctx, c, in.FolderID)
	if err != nil {
		return GetURLOutput{}, err
	}
	var segs []string
	if dir != "" {
		segs = strings.Split(dir, "/")
	}
	segs = append(segs, in.FileName)
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	// The folder URL ends with a slash; an empty fileName keeps one at the
	// end of a directory URL too.
	u := rs.Folder.URL + strings.Join(segs, "/")
	if in.Pin != "" {
		if err := pinnable(rs.Folder.PinMode); err != nil {
			return GetURLOutput{}, err
		}
		u += "?pin=" + url.QueryEscape(in.Pin)
	}
	return GetURLOutput{FolderID: in.FolderID, URL: u, ExpiresAt: formatTime(rs.Folder.ExpiresAt), FileName: in.FileName, Path: dir, PinMode: rs.Folder.PinMode}, nil
}

// pinnable checks that a url can carry a PIN for a folder guarded by mode.
func pinnable(mode string) error {
	switch mode {
	case "", entity.PinModeOff:
		return errors.New("no PIN guards the folder: leave out pin")
	case entity.PinModeTOTP:
		return errors.New("an authenticator code expires within a minute: leave out pin and let visitors enter a current code")
	default:
		return nil
	}
}

func (t *tools) folder(ctx context.Context, c caller, rawID string) (*entity.GetFolderResponse, error) {
	id, err := folderID(rawID)
	if err != nil {
		return nil, err
	}
	return t.folders.GetFolder(ctx, &entity.GetFolderRequest{UserID: c.userID, FolderID: id})
}

func folderID(raw string) (int64, error) {
	id := idgen.ParseFolderID(raw)
	if id == 0 {
		return 0, errors.New("folderId must be a folder id")
	}
	return id, nil
}

// filePath joins a directory and a file name into a folder-relative path.
func filePath(dir, fileName string) (string, error) {
	if fileName == "" || strings.Contains(fileName, "/") {
		return "", errors.New("fileName is required and must not contain /")
	}
	if dir = strings.Trim(dir, "/"); dir != "" {
		fileName = dir + "/" + fileName
	}
	if !folderctrl.ValidPath(fileName) {
		return "", folderctrl.ErrInvalidPath
	}
	return fileName, nil
}

func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return ""
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
