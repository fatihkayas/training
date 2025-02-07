Console.WriteLine("Hello\nWorld!");
Console.WriteLine("Hello\tWorld!");
Console.WriteLine("Hello \"World\"!");
//Console.WriteLine("c:\source\repos"); false
Console.WriteLine("c:\\source\\repos");

// Verbatim string literal A verbatim string literal will keep all whitespace and characters without the need to escape the backslash. To create a verbatim string, use the @ directive before the literal string.

Console.WriteLine(@"    c:\source\repos    
        (this is where your code goes)");

//What is string concatenation?String concatenation is "programmer speak" for simply combining two or more string values into a new string value. Unlike addition, the second value is appended to the end of the first value, and so on. In the following exercise, you'll write code to concatenate string values together.

string firstName = "Bob";
string message = "Hello " + firstName;
Console.WriteLine(message);



string firstName = "Bob";
string greeting = "Hello";
string message = greeting + " " + firstName + "!";
Console.WriteLine(message);


string firstName = "Bob";
string greeting = "Hello";
Console.WriteLine(greeting + " " + firstName + "!");



//What is string interpolation?String interpolation combines multiple values into a single literal string by using a "template" and one or more interpolation expressions. An interpolation expression is indicated by an opening and closing curly brace symbol { }. You can put any C# expression that returns a value inside the braces. The literal string becomes a template when it's prefixed by the $ character.

string message = $"{greeting} {firstName}!";

//Use string interpolation to combine a literal string and a variable value

string firstName = "Bob";
string message = $"Hello {firstName}!";
Console.WriteLine(message);

//Avoid intermediate variables

int version = 11;
string updateText = "Update to Windows";
Console.WriteLine($"{updateText} {version}!");


//Recap Here's what you've learned about string interpolation so far:String interpolation provides an improvement over string concatenation by reducing the number of characters required in some situations.You can combine string interpolation and verbatim literals by combining the symbols for each and using that as a prefix for the string template